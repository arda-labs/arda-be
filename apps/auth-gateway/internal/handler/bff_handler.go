package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

type BFFHandler struct {
	cfg        config.Config
	store      session.Store
	iamClient  *iamclient.Client
	logger     *slog.Logger
	httpClient *http.Client
}

func NewBFFHandler(cfg config.Config, store session.Store, iamClient *iamclient.Client) *BFFHandler {
	return &BFFHandler{
		cfg: cfg, store: store, iamClient: iamClient, logger: slog.Default(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *BFFHandler) doPut(url, contentType string, body io.Reader) (*http.Response, error) {
	req, _ := http.NewRequest("PUT", url, body)
	req.Header.Set("Content-Type", contentType)
	return h.httpClient.Do(req)
}

type loginAcceptRequest struct {
	LoginChallenge string `json:"login_challenge"`
	Subject        string `json:"subject"`
	Remember       bool   `json:"remember"`
	RememberFor    int    `json:"remember_for"`
}

type consentAcceptRequest struct {
	ConsentChallenge string `json:"consent_challenge"`
	Remember         bool   `json:"remember"`
}

type hydraTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func (h *BFFHandler) AcceptLogin(w http.ResponseWriter, r *http.Request) {
	var req loginAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
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
}

func (h *BFFHandler) ExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req tokenExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Code == "dev" {
		h.devExchangeCode(w, r)
		return
	}
	tokenURL := fmt.Sprintf("%s/oauth2/token", strings.TrimSuffix(h.cfg.HydraPublicURL, "/"))
	data := url.Values{"grant_type": {"authorization_code"}, "code": {req.Code}, "redirect_uri": {h.cfg.OAuthRedirectURI}, "client_id": {h.cfg.OAuthClientID}, "code_verifier": {req.CodeVerifier}}
	tokenReq, _ := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.httpClient.Do(tokenReq)
	if err != nil {
		respondError(w, http.StatusBadGateway, "token exchange failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respondError(w, resp.StatusCode, "token exchange failed")
		return
	}
	var tokenData hydraTokenResponse
	json.NewDecoder(resp.Body).Decode(&tokenData)
	h.createBFFSession(w, r, &tokenData)
}

func (h *BFFHandler) devExchangeCode(w http.ResponseWriter, r *http.Request) {
	userInfo, _ := h.resolveSessionUser(r.Context(), &session.UserInfo{UserID: "dev-user", Subject: "dev-user", Username: "admin", Email: "admin@arda.local"})
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	sess := &session.Session{AccessToken: "dev-token", RefreshToken: "dev-token", ExpiresAt: time.Now().Add(ttl), User: userInfo, IPAddress: extractIP(r)}
	if err := h.store.Create(r.Context(), sess, ttl); err != nil {
		respondError(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	h.setSessionCookie(w, sess.ID, ttl)
	if h.iamClient != nil && userInfo.UserID != "" {
		go h.trackSession(r, userInfo.UserID)
	}
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
				}
			}
		}
	}
	var ok bool
	userInfo, ok = h.resolveSessionUser(r.Context(), userInfo)
	if !ok || userInfo.UserID == "" {
		respondError(w, http.StatusBadGateway, "user context unavailable")
		return
	}
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	sess := &session.Session{AccessToken: tokenData.AccessToken, RefreshToken: tokenData.RefreshToken, ExpiresAt: time.Now().Add(ttl), User: userInfo, IPAddress: extractIP(r)}
	if err := h.store.Create(r.Context(), sess, ttl); err != nil {
		respondError(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	h.setSessionCookie(w, sess.ID, ttl)
	if h.iamClient != nil && userInfo.UserID != "" {
		go h.trackSession(r, userInfo.UserID)
	}
	respondJSON(w, http.StatusOK, map[string]any{"user": userInfo})
}

func (h *BFFHandler) resolveSessionUser(ctx context.Context, fallback *session.UserInfo) (*session.UserInfo, bool) {
	if fallback == nil {
		fallback = &session.UserInfo{}
	}
	if h.iamClient == nil {
		return fallback, true
	}

	if fallback.Subject != "" {
		if uc, err := h.iamClient.GetUserBySubject(ctx, fallback.Subject); err == nil {
			return sessionUserFromIAM(uc, fallback), true
		} else {
			h.logger.Warn("resolve user by subject failed", "subject", fallback.Subject, "err", err)
		}
	}

	candidates := []string{fallback.UserID, fallback.Subject}
	for _, id := range candidates {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if uc, err := h.iamClient.GetUserByID(ctx, id); err == nil {
			return sessionUserFromIAM(uc, fallback), true
		} else {
			h.logger.Warn("resolve user by id failed", "user_id", id, "err", err)
		}
	}

	return fallback, false
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
		Picture:      uc.PictureURL,
		AvatarFileID: uc.AvatarFileID,
		TenantID:     uc.TenantID,
		OrgIDs:       uc.OrgIDs,
		Roles:        uc.Roles,
		Permissions:  uc.Permissions,
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
	return info
}

func (h *BFFHandler) trackSession(r *http.Request, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ua := r.UserAgent()
	fp := iamclient.DeviceFingerprint{UserAgent: ua, IP: extractIP(r)}
	iamReq := &iamclient.CreateSessionRequest{UserID: userID, IPAddress: extractIP(r), UserAgent: ua, DeviceName: parseDeviceName(ua), DeviceType: "browser", OS: parseOS(ua), Browser: parseBrowser(ua), Fingerprint: fp.Hash()}
	if result, err := h.iamClient.CreateSession(ctx, iamReq); err != nil {
		h.logger.Warn("session tracking failed", "err", err)
	} else {
		h.logger.Info("session tracked", "session_id", result.SessionID, "device_id", result.DeviceID)
	}
}

func (h *BFFHandler) KratosWhoami(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "sessions/whoami")
}
func (h *BFFHandler) KratosCreateLoginFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/login/browser")
}
func (h *BFFHandler) KratosGetLoginFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/login/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitLogin(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/login?"+r.URL.RawQuery)
}

func (h *BFFHandler) proxyToKratos(w http.ResponseWriter, r *http.Request, path string) {
	target := fmt.Sprintf("%s/%s", strings.TrimSuffix(h.cfg.KratosPublicURL, "/"), strings.TrimPrefix(path, "/"))
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	proxyReq, _ := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	proxyReq.Header = r.Header.Clone()
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

func (h *BFFHandler) setSessionCookie(w http.ResponseWriter, id string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: h.cfg.SessionCookieName, Value: id, Path: "/", HttpOnly: true, Secure: h.cfg.CookieSecure, SameSite: parseSameSite(h.cfg.CookieSameSite), MaxAge: int(ttl.Seconds())})
}
func (h *BFFHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: h.cfg.SessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: h.cfg.CookieSecure, MaxAge: -1})
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
	userInfo, ok := h.resolveSessionUser(r.Context(), sess.User)
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
	userInfo, ok := h.resolveSessionUser(r.Context(), sess.User)
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
		h.store.Delete(r.Context(), sessionID)
		h.clearSessionCookie(w)
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *BFFHandler) Proxy(w http.ResponseWriter, r *http.Request) {
	target := h.cfg.ProxyURL() + r.URL.Path
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
	sessionID := h.readSessionCookie(r)
	if sessionID != "" {
		sess, _ := h.store.Get(r.Context(), sessionID)
		if sess != nil {
			if userInfo, ok := h.resolveSessionUser(r.Context(), sess.User); ok && userInfo.UserID != "" {
				sess.User = userInfo
				proxyReq.Header.Set("Authorization", "Bearer "+sess.AccessToken)
				proxyReq.Header.Set("X-User-Id", sess.User.UserID)
				proxyReq.Header.Set("X-User-Subject", sess.User.Subject)
				proxyReq.Header.Set("X-Username", sess.User.Username)
				proxyReq.Header.Set("X-User-Email", sess.User.Email)
				proxyReq.Header.Set("X-Tenant-Id", sess.User.TenantID)
				proxyReq.Header.Set("X-Roles", strings.Join(sess.User.Roles, ","))
				proxyReq.Header.Set("X-Permissions", strings.Join(sess.User.Permissions, ","))
				proxyReq.Header.Set("X-Auth-Checked", "true")
			}
		}
	}
	resp, err := h.httpClient.Do(proxyReq)
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
	io.Copy(w, resp.Body)
}

func (h *BFFHandler) readSessionCookie(r *http.Request) string {
	c, err := r.Cookie(h.cfg.SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip, _, ok := strings.Cut(fwd, ","); ok {
			return strings.TrimSpace(ip)
		}
	}
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

func parseDeviceName(ua string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		return "Android " + extractAndroidModel(ua)
	case strings.Contains(ua, "Windows"):
		return "Windows PC"
	case strings.Contains(ua, "Macintosh"):
		return "Mac"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	default:
		return "Unknown Device"
	}
}

func parseOS(ua string) string {
	switch {
	case strings.Contains(ua, "Windows NT 10"):
		return "Windows 10"
	case strings.Contains(ua, "Windows NT 11"):
		return "Windows 11"
	case strings.Contains(ua, "Mac OS X"):
		return "macOS"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "iPhone"):
		return "iOS"
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
	case strings.Contains(ua, "Chrome/"):
		return "Chrome"
	case strings.Contains(ua, "Firefox/"):
		return "Firefox"
	case strings.Contains(ua, "Safari/"):
		return "Safari"
	case strings.Contains(ua, "OPR/"):
		return "Opera"
	default:
		return "Unknown"
	}
}

func extractAndroidModel(ua string) string {
	if _, after, ok := strings.Cut(ua, "; "); ok {
		if end := strings.Index(after, " "); end != -1 {
			return after[:end]
		}
	}
	return ""
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

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
