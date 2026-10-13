package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

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

type oauthStateCookie struct {
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
	ReturnTo     string `json:"return_to"`
}

func (h *BFFHandler) Consent(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("consent_challenge")
	if challenge == "" {
		respondError(w, http.StatusBadRequest, "missing consent_challenge")
		return
	}
	redirectURL, ok := h.acceptHydraConsentURL(w, r, challenge, true)
	if !ok {
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (h *BFFHandler) StartOAuth(w http.ResponseWriter, r *http.Request) {
	state := randomURLToken()
	verifier := randomURLToken()
	if state == "" || verifier == "" {
		respondError(w, http.StatusInternalServerError, "oauth state generation failed")
		return
	}
	returnTo := safeReturnTo(r.URL.Query().Get("return_to"))
	stateValue, _ := json.Marshal(oauthStateCookie{
		State:        state,
		CodeVerifier: verifier,
		ReturnTo:     returnTo,
	})
	if err := h.store.SetOAuthState(r.Context(), state, string(stateValue), time.Duration(oauthStateMaxAge)*time.Second); err != nil {
		h.logger.Error("store oauth state failed", "err", err)
		respondError(w, http.StatusInternalServerError, "oauth state storage failed")
		return
	}
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {h.cfg.OAuthClientID},
		"redirect_uri":          {h.cfg.OAuthRedirectURI},
		"scope":                 {"openid email offline_access"},
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	target := strings.TrimSuffix(h.cfg.HydraPublicURL, "/") + "/oauth2/auth?" + params.Encode()
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *BFFHandler) redirectToAuthUI(w http.ResponseWriter, r *http.Request, flow, challengeParam string) {
	challenge := r.URL.Query().Get(challengeParam)
	if challenge == "" {
		respondError(w, http.StatusBadRequest, "missing "+challengeParam)
		return
	}
	req, _, err := h.getHydraAuthRequest(r.Context(), flow, challengeParam, challenge)
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
	origin, err := originFromURL(h.cfg.FrontendOrigin)
	if err != nil {
		h.logger.Error("invalid frontend origin", "frontend_origin", h.cfg.FrontendOrigin, "err", err)
		respondError(w, http.StatusInternalServerError, "frontend origin is invalid")
		return
	}
	target, _ := url.Parse(origin + "/" + flow)
	q := target.Query()
	q.Set(challengeParam, challenge)
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (h *BFFHandler) getHydraAuthRequest(ctx context.Context, flow, challengeParam, challenge string) (*hydraAuthRequest, int, error) {
	getURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/%s?%s=%s", h.cfg.HydraAdminURL, flow, challengeParam, url.QueryEscape(challenge))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("hydra returned %d: %s", resp.StatusCode, string(body))
	}
	var data hydraAuthRequest
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, resp.StatusCode, err
	}
	return &data, resp.StatusCode, nil
}

func (h *BFFHandler) AcceptConsent(w http.ResponseWriter, r *http.Request) {
	var req consentAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	redirectURL, ok := h.acceptHydraConsentURL(w, r, req.ConsentChallenge, req.Remember)
	if !ok {
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"redirect_url": redirectURL})
}

func (h *BFFHandler) acceptHydraConsentURL(w http.ResponseWriter, r *http.Request, consentChallenge string, remember bool) (string, bool) {
	if consentChallenge == "" {
		respondError(w, http.StatusBadRequest, "missing consent_challenge")
		return "", false
	}
	getURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent?consent_challenge=%s", h.cfg.HydraAdminURL, url.QueryEscape(consentChallenge))
	getReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, getURL, nil)
	if err != nil {
		respondError(w, http.StatusBadGateway, "consent request unavailable")
		return "", false
	}
	getResp, err := h.httpClient.Do(getReq)
	if err != nil {
		respondError(w, http.StatusBadGateway, "consent request unavailable")
		return "", false
	}
	defer getResp.Body.Close()
	if getResp.StatusCode >= http.StatusBadRequest {
		respondError(w, http.StatusBadGateway, "consent request unavailable")
		return "", false
	}
	var consentReq struct {
		Subject        string   `json:"subject"`
		RequestedScope []string `json:"requested_scope"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&consentReq); err != nil {
		respondError(w, http.StatusBadGateway, "invalid consent request")
		return "", false
	}
	acceptURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent/accept?consent_challenge=%s", h.cfg.HydraAdminURL, url.QueryEscape(consentChallenge))
	ab, err := json.Marshal(map[string]any{"grant_scope": consentReq.RequestedScope, "remember": remember})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "consent request encoding failed")
		return "", false
	}
	acceptReq, err := http.NewRequestWithContext(r.Context(), http.MethodPut, acceptURL, bytes.NewReader(ab))
	if err != nil {
		respondError(w, http.StatusBadGateway, "accept consent failed")
		return "", false
	}
	acceptReq.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(acceptReq)
	if err != nil {
		respondError(w, http.StatusBadGateway, "accept consent failed")
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		respondError(w, resp.StatusCode, string(body))
		return "", false
	}
	var result struct {
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		respondError(w, http.StatusBadGateway, "invalid consent response")
		return "", false
	}
	if result.RedirectTo == "" {
		respondError(w, http.StatusBadGateway, "accept consent returned empty redirect_to")
		return "", false
	}
	return result.RedirectTo, true
}

type tokenExchangeRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirect_uri"`
}

func (h *BFFHandler) Callback(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.OAuthCallback(w, r)
	case http.MethodPost:
		h.ExchangeCode(w, r)
	default:
		respondRequestError(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *BFFHandler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	callbackState := r.URL.Query().Get("state")
	if callbackState == "" {
		http.Redirect(w, r, h.frontendRedirectURL("/login?error=invalid_state"), http.StatusFound)
		return
	}
	stateValue, err := h.store.ConsumeOAuthState(r.Context(), callbackState)
	if err != nil {
		h.logger.Error("consume oauth state failed", "err", err)
		respondError(w, http.StatusInternalServerError, "oauth state unavailable")
		return
	}
	var stateCookie oauthStateCookie
	if stateValue == "" || json.Unmarshal([]byte(stateValue), &stateCookie) != nil || stateCookie.State == "" || stateCookie.CodeVerifier == "" {
		http.Redirect(w, r, h.frontendRedirectURL("/login?error=invalid_state"), http.StatusFound)
		return
	}
	if callbackState != stateCookie.State {
		http.Redirect(w, r, h.frontendRedirectURL("/login?error=invalid_state"), http.StatusFound)
		return
	}
	if errText := r.URL.Query().Get("error"); errText != "" {
		target := h.frontendRedirectURL("/login?error=" + url.QueryEscape(firstNonEmpty(r.URL.Query().Get("error_description"), errText)))
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, h.frontendRedirectURL("/login?error=missing_code"), http.StatusFound)
		return
	}
	tokenData, ok := h.exchangeHydraCode(w, r, code, stateCookie.CodeVerifier, h.cfg.OAuthRedirectURI)
	if !ok {
		return
	}
	if _, ok := h.establishBFFSession(w, r, tokenData); !ok {
		return
	}
	http.Redirect(w, r, h.frontendRedirectURL(stateCookie.ReturnTo), http.StatusFound)
}

func (h *BFFHandler) ExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req tokenExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode exchange request body", "err", err)
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.CodeVerifier) == "" {
		respondError(w, http.StatusBadRequest, "missing oauth code or verifier")
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
	tokenData, ok := h.exchangeHydraCode(w, r, req.Code, req.CodeVerifier, redirectURI)
	if !ok {
		return
	}
	h.createBFFSession(w, r, tokenData)
}

func (h *BFFHandler) exchangeHydraCode(w http.ResponseWriter, r *http.Request, code, codeVerifier, redirectURI string) (*hydraTokenResponse, bool) {
	if code == "" || codeVerifier == "" {
		respondError(w, http.StatusBadRequest, "missing oauth code or verifier")
		return nil, false
	}
	h.logger.Debug("exchanging authorization code", "redirect_uri", redirectURI)
	tokenURL := fmt.Sprintf("%s/oauth2/token", strings.TrimSuffix(h.cfg.HydraPublicURL, "/"))
	data := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {h.cfg.OAuthClientID}, "code_verifier": {codeVerifier}}
	tokenReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		respondError(w, http.StatusBadGateway, "token exchange failed")
		return nil, false
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.httpClient.Do(tokenReq)
	if err != nil {
		h.logger.Error("token exchange request failed", "err", err, "hydra_url", tokenURL)
		respondError(w, http.StatusBadGateway, "token exchange failed")
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		h.logger.Error("token exchange failed from hydra", "status", resp.StatusCode, "hydra_url", tokenURL,
			"redirect_uri", redirectURI, "client_id", h.cfg.OAuthClientID)
		respondError(w, resp.StatusCode, "token exchange failed")
		return nil, false
	}
	var tokenData hydraTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenData); err != nil {
		h.logger.Error("failed to decode token response", "err", err)
		respondError(w, http.StatusInternalServerError, "failed to decode token response")
		return nil, false
	}
	h.logger.Debug("token response decoded successfully", "expires_in", tokenData.ExpiresIn)
	return &tokenData, true
}

func (h *BFFHandler) createBFFSession(w http.ResponseWriter, r *http.Request, tokenData *hydraTokenResponse) {
	if userInfo, ok := h.establishBFFSession(w, r, tokenData); ok {
		respondJSON(w, http.StatusOK, map[string]any{"user": userInfo})
	}
}

func (h *BFFHandler) establishBFFSession(w http.ResponseWriter, r *http.Request, tokenData *hydraTokenResponse) (*session.UserInfo, bool) {
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
	userInfo, ok, _ = h.resolveSessionUser(r.Context(), userInfo, true)
	if !ok || userInfo.UserID == "" {
		h.logger.Error("user context resolution failed", "subject", userInfo.Subject)
		respondError(w, http.StatusBadGateway, "user context unavailable")
		return nil, false
	}
	h.logger.Debug("user context resolved successfully", "user_id", userInfo.UserID, "username", userInfo.Username)
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	now := time.Now()
	sess := &session.Session{AccessToken: tokenData.AccessToken, RefreshToken: tokenData.RefreshToken, ExpiresAt: now.Add(ttl), User: userInfo, IPAddress: extractIP(r), AuthTime: now, LastAuthCheck: now}
	deviceToken := h.readDeviceCookie(r)
	if deviceToken == "" {
		deviceToken = generateDeviceToken()
	}
	trustForMFA := h.readRememberMFACookie(r)
	if h.iamClient == nil || userInfo.UserID == "" {
		respondError(w, http.StatusBadGateway, "session tracking is not configured")
		return nil, false
	}
	tracked, err := h.trackSession(r, userInfo.UserID, deviceToken, trustForMFA)
	if err != nil || tracked == nil {
		if err != nil {
			h.logger.Error("session tracking failed", "err", err)
		}
		respondError(w, http.StatusBadGateway, "session tracking failed")
		return nil, false
	}
	sess.IAMSessionID = tracked.SessionID
	sess.DeviceID = tracked.DeviceID
	sess.DeviceName = tracked.DeviceName
	sess.DeviceType = tracked.DeviceType
	if err := h.store.Create(r.Context(), sess, ttl); err != nil {
		h.logger.Error("failed to create session in store", "err", err)
		if sess.IAMSessionID != "" && h.iamClient != nil {
			_ = h.iamClient.RevokeSession(r.Context(), sess.IAMSessionID)
		}
		respondError(w, http.StatusInternalServerError, "session creation failed")
		return nil, false
	}
	h.logger.Info("session created successfully", "session_id", sess.ID, "user_id", userInfo.UserID, "auth_version", userInfo.AuthVersion)
	h.setSessionCookie(w, sess.ID, ttl)
	h.setDeviceCookie(w, deviceToken)
	h.clearRememberMFACookie(w)
	return userInfo, true
}

func (h *BFFHandler) redirectToOAuthStart(w http.ResponseWriter, r *http.Request) {
	returnTo := safeReturnTo(firstNonEmpty(r.Header.Get("X-Forwarded-Uri"), r.URL.RequestURI()))
	target := "/api/auth/start?return_to=" + url.QueryEscape(returnTo)
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *BFFHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	userID := ""
	if sessionID != "" {
		if sess, _ := h.store.Get(r.Context(), sessionID); sess != nil {
			if sess.User != nil {
				userID = sess.User.UserID
			}
			if sess.IAMSessionID != "" && h.iamClient != nil {
				if err := h.iamClient.RevokeSession(r.Context(), sess.IAMSessionID); err != nil {
					h.logger.Warn("revoke iam session failed", "session_id", sess.IAMSessionID, "err", err)
				}
			}
		}
		h.store.Delete(r.Context(), sessionID)
	}
	h.clearSessionCookie(w)
	if h.logger != nil {
		h.logger.Info("session logout", "session_present", sessionID != "", "user_id", userID, "request_id", ardahttp.RequestID(r))
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func randomURLToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func safeReturnTo(value string) string {
	if value == "" {
		return "/"
	}
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "/"
	}
	return value
}

func (h *BFFHandler) frontendRedirectURL(returnTo string) string {
	safePath := safeReturnTo(returnTo)
	origin, err := originFromURL(h.cfg.FrontendOrigin)
	if err != nil {
		return safePath
	}
	return origin + safePath
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
