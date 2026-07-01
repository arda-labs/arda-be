package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/policy"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
	"github.com/arda-labs/arda/libs/go/arda-auth/permission"
)

const (
	deviceCookieName      = "arda_did"
	deviceCookieMaxAge    = 365 * 24 * 60 * 60
	rememberMFACookieName = "arda_rmf"
)

type BFFHandler struct {
	cfg              config.Config
	store            session.Store
	iamClient        *iamclient.Client
	policy           *policy.Policy
	logger           *slog.Logger
	cache            *userContextCache
	httpClient       *http.Client
	streamHTTPClient *http.Client
}

func NewBFFHandler(cfg config.Config, store session.Store, iamClient *iamclient.Client, pol *policy.Policy) *BFFHandler {
	return &BFFHandler{
		cfg: cfg, store: store, iamClient: iamClient, policy: pol, logger: slog.Default(),
		cache:            newUserContextCache(time.Duration(cfg.IAMContextCacheTTL) * time.Second),
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		streamHTTPClient: &http.Client{},
	}
}

func (h *BFFHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if h.iamClient != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.iamClient.Ready(ctx); err != nil {
			respondJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "iam": err.Error()})
			return
		}
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *BFFHandler) doPut(url, contentType string, body io.Reader) (*http.Response, error) {
	req, _ := http.NewRequest("PUT", url, body)
	req.Header.Set("Content-Type", contentType)
	return h.httpClient.Do(req)
}

type loginAcceptRequest struct {
	LoginChallenge     string `json:"login_challenge"`
	Subject            string `json:"subject"`
	Remember           bool   `json:"remember"`
	RememberFor        int    `json:"remember_for"`
	KratosSessionToken string `json:"kratos_session_token"`
}

type kratosWhoamiResponse struct {
	Identity struct {
		ID     string `json:"id"`
		Traits struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"traits"`
	} `json:"identity"`
}

type consentAcceptRequest struct {
	ConsentChallenge string `json:"consent_challenge"`
	Remember         bool   `json:"remember"`
}

type hydraAuthRequest struct {
	RequestURL string `json:"request_url"`
	Client     struct {
		ClientID string `json:"client_id"`
	} `json:"client"`
}

type hydraTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func (h *BFFHandler) AcceptKratosLogin(w http.ResponseWriter, r *http.Request) {
	var req loginAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LoginChallenge == "" {
		respondError(w, http.StatusBadRequest, "missing login_challenge")
		return
	}
	whoami, err := h.kratosWhoami(r, req.KratosSessionToken)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if h.iamClient == nil {
		respondError(w, http.StatusBadGateway, "iam client is not configured")
		return
	}
	uc, err := h.iamClient.GetUserByKratosIdentityID(r.Context(), whoami.Identity.ID)
	if err != nil {
		h.logger.Warn("resolve kratos identity failed", "identity_id", whoami.Identity.ID, "err", err)
		uc, err = h.iamClient.ResolveOrLinkKratosIdentity(r.Context(), whoami.Identity.ID, whoami.Identity.Traits.Email, whoami.Identity.Traits.Name)
		if err != nil {
			h.logger.Warn("resolve or link kratos identity failed", "identity_id", whoami.Identity.ID, "email", whoami.Identity.Traits.Email, "err", err)
			respondError(w, http.StatusBadGateway, "user context unavailable")
			return
		}
	}
	req.Subject = uc.UserID
	if !req.Remember {
		req.Remember = true
	}
	if req.RememberFor == 0 {
		req.RememberFor = int((24 * time.Hour).Seconds())
	}
	h.acceptHydraLogin(w, r, req)
}

func (h *BFFHandler) Login(w http.ResponseWriter, r *http.Request) {
	h.redirectToAuthUI(w, r, "login", "login_challenge")
}

func (h *BFFHandler) Consent(w http.ResponseWriter, r *http.Request) {
	h.redirectToAuthUI(w, r, "consent", "consent_challenge")
}

func (h *BFFHandler) redirectToAuthUI(w http.ResponseWriter, r *http.Request, flow, challengeParam string) {
	challenge := r.URL.Query().Get(challengeParam)
	if challenge == "" {
		respondError(w, http.StatusBadRequest, "missing "+challengeParam)
		return
	}
	req, err := h.getHydraAuthRequest(r.Context(), flow, challengeParam, challenge)
	if err != nil {
		h.logger.Warn("get hydra auth request failed", "flow", flow, "err", err)
		respondError(w, http.StatusBadGateway, "auth request unavailable")
		return
	}
	redirectURI, err := redirectURIFromRequestURL(req.RequestURL)
	if err != nil || !h.isAllowedOAuthRedirectURI(redirectURI) {
		h.logger.Warn("invalid auth request redirect_uri", "flow", flow, "redirect_uri", redirectURI, "err", err)
		respondError(w, http.StatusBadRequest, "redirect_uri is not allowed")
		return
	}
	origin, err := originFromURL(redirectURI)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid redirect_uri")
		return
	}
	target, _ := url.Parse(origin + "/" + flow)
	q := target.Query()
	q.Set(challengeParam, challenge)
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (h *BFFHandler) getHydraAuthRequest(ctx context.Context, flow, challengeParam, challenge string) (*hydraAuthRequest, error) {
	getURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/%s?%s=%s", h.cfg.HydraAdminURL, flow, challengeParam, url.QueryEscape(challenge))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hydra returned %d: %s", resp.StatusCode, string(body))
	}
	var data hydraAuthRequest
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (h *BFFHandler) acceptHydraLogin(w http.ResponseWriter, r *http.Request, req loginAcceptRequest) {
	hydraURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/login/accept?login_challenge=%s", h.cfg.HydraAdminURL, url.QueryEscape(req.LoginChallenge))
	b, _ := json.Marshal(map[string]any{"subject": req.Subject, "remember": req.Remember, "remember_for": req.RememberFor})
	resp, err := h.doPut(hydraURL, "application/json", bytes.NewReader(b))
	if err != nil {
		respondError(w, http.StatusBadGateway, "accept login failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		respondError(w, resp.StatusCode, string(body))
		return
	}
	var result struct {
		RedirectTo string `json:"redirect_to"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.RedirectTo == "" {
		respondError(w, http.StatusBadGateway, "accept login returned empty redirect_to")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"redirect_url": result.RedirectTo})
}

func (h *BFFHandler) AcceptConsent(w http.ResponseWriter, r *http.Request) {
	var req consentAcceptRequest
	json.NewDecoder(r.Body).Decode(&req)
	getURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent?consent_challenge=%s", h.cfg.HydraAdminURL, url.QueryEscape(req.ConsentChallenge))
	getResp, _ := h.httpClient.Get(getURL)
	if getResp != nil {
		defer getResp.Body.Close()
	}
	var consentReq struct {
		Subject        string   `json:"subject"`
		RequestedScope []string `json:"requested_scope"`
	}
	if getResp != nil {
		rbody, _ := io.ReadAll(getResp.Body)
		json.Unmarshal(rbody, &consentReq)
	}
	acceptURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent/accept?consent_challenge=%s", h.cfg.HydraAdminURL, url.QueryEscape(req.ConsentChallenge))
	ab, _ := json.Marshal(map[string]any{"grant_scope": consentReq.RequestedScope, "remember": req.Remember})
	resp, err := h.doPut(acceptURL, "application/json", bytes.NewReader(ab))
	if err != nil {
		respondError(w, http.StatusBadGateway, "accept consent failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		respondError(w, resp.StatusCode, string(body))
		return
	}
	var result struct {
		RedirectTo string `json:"redirect_to"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.RedirectTo == "" {
		respondError(w, http.StatusBadGateway, "accept consent returned empty redirect_to")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"redirect_url": result.RedirectTo})
}

type tokenExchangeRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirect_uri"`
}

func (h *BFFHandler) ExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req tokenExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode exchange request body", "err", err)
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Code == "dev" {
		h.devExchangeCode(w, r)
		return
	}
	redirectURI := req.RedirectURI
	if redirectURI == "" {
		redirectURI = h.cfg.OAuthRedirectURI
	}
	if !h.isAllowedOAuthRedirectURI(redirectURI) {
		respondError(w, http.StatusBadRequest, "redirect_uri is not allowed")
		return
	}
	h.logger.Debug("exchanging authorization code", "code", req.Code, "redirect_uri", redirectURI)
	tokenURL := fmt.Sprintf("%s/oauth2/token", strings.TrimSuffix(h.cfg.HydraPublicURL, "/"))
	data := url.Values{"grant_type": {"authorization_code"}, "code": {req.Code}, "redirect_uri": {redirectURI}, "client_id": {h.cfg.OAuthClientID}, "code_verifier": {req.CodeVerifier}}
	tokenReq, _ := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.httpClient.Do(tokenReq)
	if err != nil {
		h.logger.Error("token exchange request failed", "err", err, "hydra_url", tokenURL)
		respondError(w, http.StatusBadGateway, "token exchange failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		h.logger.Error("token exchange failed from hydra", "status", resp.StatusCode, "hydra_url", tokenURL,
			"redirect_uri", redirectURI, "client_id", h.cfg.OAuthClientID,
			"hydra_response", string(bodyBytes))
		respondError(w, resp.StatusCode, "token exchange failed: "+string(bodyBytes))
		return
	}
	var tokenData hydraTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenData); err != nil {
		h.logger.Error("failed to decode token response", "err", err)
		respondError(w, http.StatusInternalServerError, "failed to decode token response")
		return
	}
	h.logger.Debug("token response decoded successfully", "expires_in", tokenData.ExpiresIn)
	h.createBFFSession(w, r, &tokenData)
}

func (h *BFFHandler) devExchangeCode(w http.ResponseWriter, r *http.Request) {
	userInfo, _ := h.resolveSessionUser(r.Context(), &session.UserInfo{UserID: "dev-user", Subject: "dev-user", Username: "admin", Email: "admin@arda.local"}, true)
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	now := time.Now()
	sess := &session.Session{AccessToken: "dev-token", RefreshToken: "dev-token", ExpiresAt: now.Add(ttl), User: userInfo, IPAddress: extractIP(r), AuthTime: now}
	deviceToken := h.readDeviceCookie(r)
	if deviceToken == "" {
		deviceToken = generateDeviceToken()
	}
	trustForMFA := h.readRememberMFACookie(r)
	if h.iamClient != nil && userInfo.UserID != "" {
		if tracked, err := h.trackSession(r, userInfo.UserID, deviceToken, trustForMFA); err != nil {
			h.logger.Warn("session tracking failed", "err", err)
		} else if tracked != nil {
			sess.IAMSessionID = tracked.SessionID
			sess.DeviceID = tracked.DeviceID
			sess.DeviceName = tracked.DeviceName
			sess.DeviceType = tracked.DeviceType
		}
	}
	if err := h.store.Create(r.Context(), sess, ttl); err != nil {
		if sess.IAMSessionID != "" && h.iamClient != nil {
			_ = h.iamClient.RevokeSession(r.Context(), sess.IAMSessionID)
		}
		respondError(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	h.setSessionCookie(w, sess.ID, ttl)
	h.setDeviceCookie(w, deviceToken)
	h.clearRememberMFACookie(w)
	respondJSON(w, http.StatusOK, map[string]any{"user": userInfo})
}

func (h *BFFHandler) createBFFSession(w http.ResponseWriter, r *http.Request, tokenData *hydraTokenResponse) {
	userInfo := &session.UserInfo{}
	if tokenData.IDToken != "" {
		parts := strings.Split(tokenData.IDToken, ".")
		if len(parts) == 3 {
			if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
				var claims struct {
					Sub      string `json:"sub"`
					Email    string `json:"email"`
					Username string `json:"username"`
				}
				if json.Unmarshal(payload, &claims) == nil {
					userInfo = &session.UserInfo{Subject: claims.Sub, Username: claims.Username, Email: claims.Email}
					h.logger.Debug("extracted claims from ID token", "sub", claims.Sub, "email", claims.Email)
				} else {
					h.logger.Warn("failed to unmarshal ID token payload claims")
				}
			} else {
				h.logger.Warn("failed to decode ID token payload base64", "err", err)
			}
		} else {
			h.logger.Warn("invalid ID token parts count", "parts", len(parts))
		}
	} else {
		h.logger.Warn("id token is empty in hydra token response")
	}
	var ok bool
	userInfo, ok = h.resolveSessionUser(r.Context(), userInfo, true)
	if !ok || userInfo.UserID == "" {
		h.logger.Error("user context resolution failed", "subject", userInfo.Subject)
		respondError(w, http.StatusBadGateway, "user context unavailable")
		return
	}
	h.logger.Debug("user context resolved successfully", "user_id", userInfo.UserID, "username", userInfo.Username)
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	now := time.Now()
	sess := &session.Session{AccessToken: tokenData.AccessToken, RefreshToken: tokenData.RefreshToken, ExpiresAt: now.Add(ttl), User: userInfo, IPAddress: extractIP(r), AuthTime: now}
	deviceToken := h.readDeviceCookie(r)
	if deviceToken == "" {
		deviceToken = generateDeviceToken()
	}
	trustForMFA := h.readRememberMFACookie(r)
	if h.iamClient != nil && userInfo.UserID != "" {
		if tracked, err := h.trackSession(r, userInfo.UserID, deviceToken, trustForMFA); err != nil {
			h.logger.Warn("session tracking failed", "err", err)
		} else if tracked != nil {
			sess.IAMSessionID = tracked.SessionID
			sess.DeviceID = tracked.DeviceID
			sess.DeviceName = tracked.DeviceName
			sess.DeviceType = tracked.DeviceType
		}
	}
	if err := h.store.Create(r.Context(), sess, ttl); err != nil {
		h.logger.Error("failed to create session in store", "err", err)
		if sess.IAMSessionID != "" && h.iamClient != nil {
			_ = h.iamClient.RevokeSession(r.Context(), sess.IAMSessionID)
		}
		respondError(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	h.logger.Info("session created successfully", "session_id", sess.ID, "user_id", userInfo.UserID)
	h.setSessionCookie(w, sess.ID, ttl)
	h.setDeviceCookie(w, deviceToken)
	h.clearRememberMFACookie(w)
	respondJSON(w, http.StatusOK, map[string]any{"user": userInfo})
}

func (h *BFFHandler) resolveSessionUser(_ context.Context, fallback *session.UserInfo, useCache bool) (*session.UserInfo, bool) {
	if fallback == nil {
		fallback = &session.UserInfo{}
	}
	if h.iamClient == nil {
		return fallback, true
	}

	if fallback.UserID == "" && looksLikeUUID(fallback.Subject) {
		fallback.UserID = fallback.Subject
	}
	if useCache && h.cache != nil {
		for _, key := range sessionUserCacheKeys(fallback.UserID, fallback.Subject, fallback.AuthVersion) {
			if uc, ok := h.cache.get(key); ok {
				return sessionUserFromIAM(uc, fallback), true
			}
		}
	}

	lookupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if fallback.Subject != "" && !looksLikeUUID(fallback.Subject) {
		if uc, err := h.iamClient.GetUserBySubject(lookupCtx, fallback.Subject); err == nil {
			h.cacheSessionUser(fallback, uc)
			return sessionUserFromIAM(uc, fallback), true
		} else {
			h.logger.Warn("resolve user by subject failed", "subject", fallback.Subject, "err", err)
		}
	}

	for _, id := range iamLookupIDs(fallback) {
		if uc, err := h.iamClient.GetUserByID(lookupCtx, id); err == nil {
			h.cacheSessionUser(fallback, uc)
			return sessionUserFromIAM(uc, fallback), true
		} else {
			h.logger.Warn("resolve user by id failed", "user_id", id, "err", err)
		}
	}

	return fallback, false
}

func (h *BFFHandler) cacheSessionUser(fallback *session.UserInfo, uc *iamclient.UserContext) {
	if h.cache == nil || uc == nil {
		return
	}
	for _, key := range sessionUserCacheKeys(uc.UserID, uc.Subject, uc.AuthVersion) {
		h.cache.set(key, uc)
	}
	if fallback != nil && fallback.AuthVersion == uc.AuthVersion {
		for _, key := range sessionUserCacheKeys(fallback.UserID, fallback.Subject, fallback.AuthVersion) {
			h.cache.set(key, uc)
		}
	}
}

func sessionUserCacheKeys(userID, subject string, authVersion int64) []string {
	if authVersion <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var keys []string
	for _, base := range []string{strings.TrimSpace(userID), strings.TrimSpace(subject)} {
		if base == "" {
			continue
		}
		key := fmt.Sprintf("%s:v%d", base, authVersion)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func iamLookupIDs(info *session.UserInfo) []string {
	if info == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var ids []string
	for _, id := range []string{info.UserID, info.Subject} {
		id = strings.TrimSpace(id)
		if id == "" || !looksLikeUUID(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func sessionUserFromIAM(uc *iamclient.UserContext, fallback *session.UserInfo) *session.UserInfo {
	if uc == nil {
		return fallback
	}
	info := &session.UserInfo{
		UserID:       uc.UserID,
		Subject:      uc.Subject,
		Username:     uc.Username,
		Email:        uc.Email,
		DisplayName:  uc.DisplayName,
		Nickname:     uc.Nickname,
		FirstName:    uc.FirstName,
		LastName:     uc.LastName,
		PhoneNumber:  uc.PhoneNumber,
		Birthdate:    uc.Birthdate,
		Gender:       uc.Gender,
		Address:      uc.Address,
		Country:      uc.Country,
		Picture:      uc.PictureURL,
		AvatarFileID: uc.AvatarFileID,
		CoverImage:   uc.CoverImageURL,
		CoverFileID:  uc.CoverFileID,
		TenantID:     uc.TenantID,
		OrgIDs:       uc.OrgIDs,
		Roles:        uc.Roles,
		Permissions:  uc.Permissions,
		AuthVersion:  uc.AuthVersion,
	}
	if info.Subject == "" && fallback != nil {
		info.Subject = fallback.Subject
	}
	if info.Username == "" && fallback != nil {
		info.Username = fallback.Username
	}
	if info.Email == "" && fallback != nil {
		info.Email = fallback.Email
	}
	if info.DisplayName == "" && fallback != nil {
		info.DisplayName = fallback.DisplayName
	}
	if info.Nickname == "" && fallback != nil {
		info.Nickname = fallback.Nickname
	}
	if info.FirstName == "" && fallback != nil {
		info.FirstName = fallback.FirstName
	}
	if info.LastName == "" && fallback != nil {
		info.LastName = fallback.LastName
	}
	if info.PhoneNumber == "" && fallback != nil {
		info.PhoneNumber = fallback.PhoneNumber
	}
	if info.Birthdate == "" && fallback != nil {
		info.Birthdate = fallback.Birthdate
	}
	if info.Gender == "" && fallback != nil {
		info.Gender = fallback.Gender
	}
	if info.Address == "" && fallback != nil {
		info.Address = fallback.Address
	}
	if info.Country == "" && fallback != nil {
		info.Country = fallback.Country
	}
	if info.CoverImage == "" && fallback != nil {
		info.CoverImage = fallback.CoverImage
	}
	if info.CoverFileID == "" && fallback != nil {
		info.CoverFileID = fallback.CoverFileID
	}
	return info
}

type trackedSession struct {
	SessionID  string
	DeviceID   string
	DeviceName string
	DeviceType string
}

func (h *BFFHandler) trackSession(r *http.Request, userID, deviceToken string, trustForMFA bool) (*trackedSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ua := r.UserAgent()
	fp := iamclient.DeviceFingerprint{UserAgent: ua, IP: extractIP(r)}
	deviceType := parseDeviceType(ua)
	osName := parseOS(ua)
	browserName := parseBrowser(ua)
	deviceName := parseDeviceName(ua, deviceType, osName, browserName)
	iamReq := &iamclient.CreateSessionRequest{
		UserID:      userID,
		IPAddress:   extractIP(r),
		UserAgent:   ua,
		DeviceName:  deviceName,
		DeviceType:  deviceType,
		OS:          osName,
		Browser:     browserName,
		Fingerprint: fp.Hash(),
		DeviceToken: deviceToken,
		TrustForMFA: trustForMFA,
	}
	result, err := h.iamClient.CreateSession(ctx, iamReq)
	if err != nil {
		return nil, err
	}
	h.logger.Info("session tracked", "session_id", result.SessionID, "device_id", result.DeviceID)
	return &trackedSession{
		SessionID:  result.SessionID,
		DeviceID:   result.DeviceID,
		DeviceName: deviceName,
		DeviceType: deviceType,
	}, nil
}

func (h *BFFHandler) verifyMFA(ctx context.Context, userID, code string) error {
	if h.iamClient == nil {
		return fmt.Errorf("iam client is not configured")
	}
	body, err := json.Marshal(map[string]string{
		"userId": userID,
		"code":   code,
	})
	if err != nil {
		return err
	}
	endpoint := h.iamClient.InternalBaseURL() + "/api/iam/me/mfa/verify"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iam mfa verify returned status %d", resp.StatusCode)
	}
	return nil
}

func (h *BFFHandler) KratosWhoami(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "sessions/whoami")
}
func (h *BFFHandler) KratosCreateLoginAPIFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratosAPI(w, r, "self-service/login/api")
}
func (h *BFFHandler) KratosCreateLoginFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/login/browser")
}
func (h *BFFHandler) KratosGetLoginFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/login/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitLogin(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratosAPI(w, r, "self-service/login?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosCreateSettingsFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/settings/browser")
}
func (h *BFFHandler) KratosGetSettingsFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/settings/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitSettings(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/settings?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosCreateRecoveryFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/recovery/browser")
}
func (h *BFFHandler) KratosGetRecoveryFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/recovery/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitRecovery(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/recovery?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosCreateVerificationFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/verification/browser")
}
func (h *BFFHandler) KratosGetVerificationFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/verification/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitVerification(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/verification?"+r.URL.RawQuery)
}

func (h *BFFHandler) proxyToKratos(w http.ResponseWriter, r *http.Request, path string) {
	h.proxyToKratosWithOptions(w, r, path, false)
}

func (h *BFFHandler) proxyToKratosAPI(w http.ResponseWriter, r *http.Request, path string) {
	h.proxyToKratosWithOptions(w, r, path, true)
}

func (h *BFFHandler) proxyToKratosWithOptions(w http.ResponseWriter, r *http.Request, path string, stripBrowserHeaders bool) {
	target := fmt.Sprintf("%s/%s", strings.TrimSuffix(h.cfg.KratosPublicURL, "/"), strings.TrimPrefix(path, "/"))
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	proxyReq, _ := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	proxyReq.Header = r.Header.Clone()
	if stripBrowserHeaders {
		// Kratos API flows reject browser AJAX requests with Origin/Sec-Fetch headers.
		// The BFF is intentionally converting this same-origin browser call into a
		// server-to-server API-flow request so Kratos can return a session_token.
		proxyReq.Header.Del("Origin")
		proxyReq.Header.Del("Referer")
		proxyReq.Header.Del("Sec-Fetch-Dest")
		proxyReq.Header.Del("Sec-Fetch-Mode")
		proxyReq.Header.Del("Sec-Fetch-Site")
		proxyReq.Header.Del("Cookie")
	}
	proxyReq.Header.Set("X-Forwarded-Proto", "https")
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		respondError(w, http.StatusBadGateway, "kratos proxy failed")
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (h *BFFHandler) kratosWhoami(r *http.Request, sessionToken string) (*kratosWhoamiResponse, error) {
	target := fmt.Sprintf("%s/%s", strings.TrimSuffix(h.cfg.KratosPublicURL, "/"), "sessions/whoami")
	proxyReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	proxyReq.Header = r.Header.Clone()
	proxyReq.Header.Set("Accept", "application/json")
	proxyReq.Header.Del("Accept-Encoding")
	if strings.TrimSpace(sessionToken) != "" {
		proxyReq.Header.Set("X-Session-Token", strings.TrimSpace(sessionToken))
	}
	proxyReq.Header.Set("X-Forwarded-Proto", "https")
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("kratos whoami failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kratos session is not authenticated: HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}
	var whoami kratosWhoamiResponse
	if err := json.Unmarshal(body, &whoami); err != nil {
		return nil, fmt.Errorf("decode kratos whoami failed: %s", truncateBody(body))
	}
	if strings.TrimSpace(whoami.Identity.ID) == "" {
		return nil, fmt.Errorf("kratos whoami missing identity: %s", truncateBody(body))
	}
	return &whoami, nil
}

func truncateBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		return text[:500]
	}
	if text == "" {
		return "<empty response>"
	}
	return text
}

func (h *BFFHandler) setSessionCookie(w http.ResponseWriter, id string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: h.cfg.SessionCookieName, Value: id, Path: "/", HttpOnly: true, Secure: h.cfg.CookieSecure, SameSite: parseSameSite(h.cfg.CookieSameSite), MaxAge: int(ttl.Seconds())})
}

func (h *BFFHandler) setDeviceCookie(w http.ResponseWriter, token string) {
	if token == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     deviceCookieName,
		Value:    token,
		Path:     "/",
		Domain:   h.cfg.SessionCookieDomain,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: parseSameSite(h.cfg.CookieSameSite),
		MaxAge:   deviceCookieMaxAge,
	})
}

func (h *BFFHandler) clearRememberMFACookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     rememberMFACookieName,
		Value:    "",
		Path:     "/",
		Domain:   h.cfg.SessionCookieDomain,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: parseSameSite(h.cfg.CookieSameSite),
		MaxAge:   -1,
	})
}

func (h *BFFHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: parseSameSite(h.cfg.CookieSameSite),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (h *BFFHandler) Me(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	userInfo, ok := h.resolveSessionUser(r.Context(), sess.User, true)
	if !ok || userInfo.UserID == "" {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "user context unavailable")
		return
	}
	sess.User = userInfo
	respondJSON(w, http.StatusOK, sess.User)
}

func (h *BFFHandler) MeSessions(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil || sess.User == nil {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	userInfo, ok := h.resolveSessionUser(r.Context(), sess.User, true)
	if !ok || userInfo.UserID == "" {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "user context unavailable")
		return
	}
	sess.User = userInfo
	sessions, _ := h.store.ListByUser(r.Context(), sess.User.UserID)
	respondJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "current": sessionID})
}

func (h *BFFHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID != "" {
		if sess, _ := h.store.Get(r.Context(), sessionID); sess != nil && sess.IAMSessionID != "" && h.iamClient != nil {
			if err := h.iamClient.RevokeSession(r.Context(), sess.IAMSessionID); err != nil {
				h.logger.Warn("revoke iam session failed", "session_id", sess.IAMSessionID, "err", err)
			}
		}
		h.store.Delete(r.Context(), sessionID)
	}
	h.clearSessionCookie(w)
	respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *BFFHandler) StepUp(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil || sess.User == nil || sess.User.UserID == "" {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		respondError(w, http.StatusBadRequest, "code is required")
		return
	}
	if err := h.verifyMFA(r.Context(), sess.User.UserID, strings.TrimSpace(req.Code)); err != nil {
		respondError(w, http.StatusUnauthorized, "invalid MFA code")
		return
	}
	sess.AuthTime = time.Now()
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if err := h.store.Refresh(r.Context(), sessionID, sess, ttl); err != nil {
		respondError(w, http.StatusInternalServerError, "refresh session failed")
		return
	}
	h.setSessionCookie(w, sess.ID, ttl)
	respondJSON(w, http.StatusOK, map[string]any{
		"status":       "verified",
		"stepUpUntil":  sess.AuthTime.Add(time.Duration(h.cfg.RecentAuthWindow) * time.Second).Format(time.RFC3339),
		"validSeconds": h.cfg.RecentAuthWindow,
	})
}

func (h *BFFHandler) Proxy(w http.ResponseWriter, r *http.Request) {
	baseURL := h.upstreamBaseURL(r.URL.Path)
	target := baseURL + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	proxyReq, _ := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	for k, vs := range r.Header {
		for _, v := range vs {
			proxyReq.Header.Add(k, v)
		}
	}
	stripAuthContextHeaders(proxyReq.Header)
	var match *policy.MatchResult
	if h.policy != nil {
		match, _ = h.policy.Match(r.URL.Path, r.Method)
	}
	sessionID := h.readSessionCookie(r)
	var sess *session.Session
	if sessionID != "" {
		sess, _ = h.store.Get(r.Context(), sessionID)
	}
	requireAuth := match == nil || match.RequireAuth
	if requireAuth && sess == nil {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if sess != nil {
		if !h.recentAuthOK(r, sess) {
			respondError(w, http.StatusForbidden, "recent_auth_required")
			return
		}
		userInfo, ok := h.resolveSessionUser(r.Context(), sess.User, match == nil || match.Route.Risk != "high")
		if !ok || userInfo.UserID == "" {
			if requireAuth {
				h.clearSessionCookie(w)
				respondError(w, http.StatusUnauthorized, "user context unavailable")
				return
			}
		} else {
			sess.User = userInfo
			if match != nil && len(match.Route.Permissions) > 0 && !permission.HasAny(sess.User.Permissions, match.Route.Permissions...) {
				respondError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			proxyReq.Header.Set("Authorization", "Bearer "+sess.AccessToken)
			proxyReq.Header.Set("X-User-Id", sess.User.UserID)
			proxyReq.Header.Set("X-User-Subject", sess.User.Subject)
			proxyReq.Header.Set("X-Username", sess.User.Username)
			proxyReq.Header.Set("X-User-Email", sess.User.Email)
			proxyReq.Header.Set("X-Nickname", sess.User.Nickname)
			proxyReq.Header.Set("X-Tenant-Id", sess.User.TenantID)
			proxyReq.Header.Set("X-Roles", strings.Join(sess.User.Roles, ","))
			proxyReq.Header.Set("X-Permissions", strings.Join(sess.User.Permissions, ","))
			proxyReq.Header.Set("X-Auth-Version", fmt.Sprintf("%d", sess.User.AuthVersion))
			if !sess.AuthTime.IsZero() {
				proxyReq.Header.Set("X-Auth-Time", fmt.Sprintf("%d", sess.AuthTime.Unix()))
			}
			if sess.IAMSessionID != "" {
				proxyReq.Header.Set("X-Session-Id", sess.IAMSessionID)
			}
			if match != nil {
				proxyReq.Header.Set("X-Auth-Risk", match.Route.Risk)
			}
			proxyReq.Header.Set("X-Auth-Checked", "true")
		}
	}
	client := h.httpClient
	if isEventStreamRequest(r) {
		client = h.streamHTTPClient
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		respondError(w, http.StatusBadGateway, "upstream error")
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if isEventStreamRequest(r) {
		copyEventStream(w, resp.Body)
		return
	}
	io.Copy(w, resp.Body)
}

func isEventStreamRequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func copyEventStream(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func (h *BFFHandler) upstreamBaseURL(path string) string {
	for _, route := range []struct {
		prefix string
		url    string
	}{
		{"/api/admin", h.cfg.IAMServiceURL},
		{"/api/iam", h.cfg.IAMServiceURL},
		{"/api/platform", h.cfg.PlatformServiceURL},
		{"/api/finance", h.cfg.FinanceServiceURL},
		{"/api/media", h.cfg.MediaServiceURL},
		{"/api/workflow", h.cfg.WorkflowServiceURL},
		{"/api/crm", h.cfg.CRMServiceURL},
		{"/api/notifications", h.cfg.NotificationURL},
		{"/api/mdm", h.cfg.MDMServiceURL},
	} {
		if strings.HasPrefix(path, route.prefix) && strings.TrimSpace(route.url) != "" {
			return route.url
		}
	}
	return h.cfg.ProxyURL()
}

func (h *BFFHandler) recentAuthOK(r *http.Request, sess *session.Session) bool {
	if h.cfg.RecentAuthWindow <= 0 || sess == nil || h.policy == nil {
		return true
	}
	match, err := h.policy.Match(r.URL.Path, r.Method)
	if err != nil || match.Route.Risk != "high" {
		return true
	}
	if sess.AuthTime.IsZero() {
		return false
	}
	return time.Since(sess.AuthTime) <= time.Duration(h.cfg.RecentAuthWindow)*time.Second
}

func stripAuthContextHeaders(header http.Header) {
	for _, key := range []string{
		"X-User-Id",
		"X-User-Subject",
		"X-Username",
		"X-User-Email",
		"X-Nickname",
		"X-Tenant-Id",
		"X-Roles",
		"X-Permissions",
		"X-Session-Id",
		"X-Auth-Version",
		"X-Auth-Time",
		"X-Auth-Risk",
		"X-Auth-Checked",
	} {
		header.Del(key)
	}
}

func (h *BFFHandler) readSessionCookie(r *http.Request) string {
	c, err := r.Cookie(h.cfg.SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (h *BFFHandler) readDeviceCookie(r *http.Request) string {
	c, err := r.Cookie(deviceCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (h *BFFHandler) readRememberMFACookie(r *http.Request) bool {
	c, err := r.Cookie(rememberMFACookieName)
	if err != nil {
		return false
	}
	return c.Value == "1"
}

func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip, _, ok := strings.Cut(fwd, ","); ok {
			return normalizeIP(strings.TrimSpace(ip))
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return normalizeIP(host)
	}
	return normalizeIP(r.RemoteAddr)
}

func parseDeviceType(ua string) string {
	switch {
	case strings.Contains(ua, "iPad"), strings.Contains(ua, "Tablet"):
		return "tablet"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "Android"), strings.Contains(ua, "Mobile"):
		return "mobile"
	default:
		return "browser"
	}
}

func parseDeviceName(ua, deviceType, osName, browserName string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		if model := extractAndroidModel(ua); model != "" {
			return model
		}
		if deviceType == "tablet" {
			return "Android Tablet"
		}
		return "Android Phone"
	case browserName != "Unknown" && osName != "Unknown":
		return browserName + " on " + osName
	case osName != "Unknown":
		switch deviceType {
		case "mobile":
			return osName + " Phone"
		case "tablet":
			return osName + " Tablet"
		default:
			return osName + " Device"
		}
	default:
		return "Web Browser"
	}
}

func parseOS(ua string) string {
	switch {
	case strings.Contains(ua, "Windows NT"):
		return "Windows"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		return "iOS"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "Mac OS X"):
		return "macOS"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	default:
		return "Unknown"
	}
}

func parseBrowser(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/"):
		return "Edge"
	case strings.Contains(ua, "OPR/"):
		return "Opera"
	case strings.Contains(ua, "SamsungBrowser/"):
		return "Samsung Internet"
	case strings.Contains(ua, "CriOS/"):
		return "Chrome"
	case strings.Contains(ua, "Chrome/"):
		return "Chrome"
	case strings.Contains(ua, "Firefox/"):
		return "Firefox"
	case strings.Contains(ua, "Version/") && strings.Contains(ua, "Safari/"):
		return "Safari"
	default:
		return "Unknown"
	}
}

func extractAndroidModel(ua string) string {
	parts := strings.Split(ua, ";")
	for _, part := range parts {
		segment := strings.TrimSpace(part)
		switch {
		case segment == "":
			continue
		case strings.HasPrefix(segment, "Linux"):
			continue
		case strings.HasPrefix(segment, "Android"):
			continue
		case segment == "wv":
			continue
		}

		if idx := strings.Index(segment, " Build/"); idx != -1 {
			segment = segment[:idx]
		}
		if segment != "" {
			return segment
		}
	}
	return ""
}

func generateDeviceToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return session.NewID()
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func looksLikeUUID(value string) bool {
	return len(value) == 36 && strings.Count(value, "-") == 4
}

func normalizeIP(value string) string {
	return strings.Trim(value, "[]")
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

func (h *BFFHandler) isAllowedOAuthRedirectURI(redirectURI string) bool {
	if redirectURI == "" {
		return false
	}
	for _, allowed := range strings.Split(h.cfg.OAuthRedirectURIs, ",") {
		if strings.TrimSpace(allowed) == redirectURI {
			return true
		}
	}
	return h.cfg.OAuthRedirectURI == redirectURI
}

func redirectURIFromRequestURL(requestURL string) (string, error) {
	u, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	redirectURI := u.Query().Get("redirect_uri")
	if redirectURI == "" {
		return "", fmt.Errorf("missing redirect_uri")
	}
	return redirectURI, nil
}

func originFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme")
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	return u.Scheme + "://" + u.Host, nil
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
