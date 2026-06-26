package handler

import (
	"bytes"
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
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

// BFFHandler manages BFF endpoints.
type BFFHandler struct {
	cfg        config.Config
	store      session.Store
	logger     *slog.Logger
	httpClient *http.Client
}

// NewBFFHandler creates a new BFF handler.
func NewBFFHandler(cfg config.Config, store session.Store) *BFFHandler {
	return &BFFHandler{
		cfg:    cfg,
		store:  store,
		logger: slog.Default(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ── Hydra Admin endpoints (proxied) ──

type (
	loginAcceptRequest struct {
		LoginChallenge string `json:"login_challenge"`
		Subject        string `json:"subject"`
		Remember       bool   `json:"remember"`
		RememberFor    int    `json:"remember_for"`
	}

	consentAcceptRequest struct {
		ConsentChallenge string `json:"consent_challenge"`
		Remember         bool   `json:"remember"`
	}
)

// AcceptLogin calls Hydra Admin API to accept a login request.
func (h *BFFHandler) AcceptLogin(w http.ResponseWriter, r *http.Request) {
	var req loginAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	hydraURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/login/accept?login_challenge=%s",
		h.cfg.HydraAdminURL, url.QueryEscape(req.LoginChallenge))

	body := map[string]any{
		"subject":      req.Subject,
		"remember":     req.Remember,
		"remember_for": req.RememberFor,
	}

	resp, err := h.putJSON(hydraURL, body)
	if err != nil {
		h.logger.Error("accept login", "err", err)
		respondError(w, http.StatusBadGateway, "accept login failed")
		return
	}
	defer resp.Body.Close()

	var result struct {
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		respondError(w, http.StatusInternalServerError, "decode response failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"redirect_url": result.RedirectTo,
	})
}

// AcceptConsent calls Hydra Admin API to auto-accept consent.
func (h *BFFHandler) AcceptConsent(w http.ResponseWriter, r *http.Request) {
	var req consentAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.logger.Info("accept consent called",
		"consent_challenge", req.ConsentChallenge[:40]+"...",
		"remember", req.Remember,
	)

	getURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent?consent_challenge=%s",
		h.cfg.HydraAdminURL, url.QueryEscape(req.ConsentChallenge))

	getResp, err := h.httpClient.Get(getURL)
	if err != nil {
		h.logger.Error("get consent request", "err", err)
		respondError(w, http.StatusBadGateway, "get consent request failed")
		return
	}
	defer getResp.Body.Close()

	body, _ := io.ReadAll(getResp.Body)

	if getResp.StatusCode != http.StatusOK {
		respondError(w, http.StatusBadGateway, "consent request not found or expired")
		return
	}

	var consentReq struct {
		Subject        string   `json:"subject"`
		RequestedScope []string `json:"requested_scope"`
	}
	if err := json.Unmarshal(body, &consentReq); err != nil {
		respondError(w, http.StatusInternalServerError, "decode consent request failed")
		return
	}

	acceptURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent/accept?consent_challenge=%s",
		h.cfg.HydraAdminURL, url.QueryEscape(req.ConsentChallenge))

	resp, err := h.putJSON(acceptURL, map[string]any{
		"grant_scope": consentReq.RequestedScope,
		"remember":    req.Remember,
	})
	if err != nil {
		h.logger.Error("accept consent put", "err", err)
		respondError(w, http.StatusBadGateway, "accept consent failed")
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		respondError(w, resp.StatusCode, "hydra rejected consent")
		return
	}

	var result struct {
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		respondError(w, http.StatusInternalServerError, "decode response failed")
		return
	}

	h.logger.Info("consent accepted, redirecting", "to", result.RedirectTo[:70]+"...")
	respondJSON(w, http.StatusOK, map[string]string{"redirect_url": result.RedirectTo})
}

// ── Token exchange (BFF — tokens NEVER exposed to browser) ──

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

	tokenURL := fmt.Sprintf("%s/oauth2/token", strings.TrimSuffix(h.cfg.HydraPublicURL, "/"))
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {req.Code},
		"redirect_uri":  {h.cfg.OAuthRedirectURI},
		"client_id":     {h.cfg.OAuthClientID},
		"code_verifier": {req.CodeVerifier},
	}

	tokenReq, _ := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(tokenReq)
	if err != nil {
		respondError(w, http.StatusBadGateway, "token exchange failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		h.logger.Warn("token exchange failed", "status", resp.StatusCode, "body", string(body))
		respondError(w, resp.StatusCode, "token exchange failed")
		return
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenData); err != nil {
		respondError(w, http.StatusInternalServerError, "decode response failed")
		return
	}

	// Parse user info from ID token (full token never sent to browser)
	userInfo := &session.UserInfo{}
	if tokenData.IDToken != "" {
		parts := strings.Split(tokenData.IDToken, ".")
		if len(parts) == 3 {
			if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
				var claims struct {
					Sub      string   `json:"sub"`
					Email    string   `json:"email"`
					Username string   `json:"username"`
				}
				if json.Unmarshal(payload, &claims) == nil {
					userInfo = &session.UserInfo{
						UserID:   claims.Sub,
						Subject:  claims.Sub,
						Username: claims.Username,
						Email:    claims.Email,
					}
				}
			}
		}
	}

	// Create BFF session — tokens stay server-side only
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	sess := &session.Session{
		AccessToken:  tokenData.AccessToken,
		RefreshToken: tokenData.RefreshToken,
		ExpiresAt:    time.Now().Add(ttl),
		User:         userInfo,
	}
	if err := h.store.Create(r.Context(), sess, ttl); err != nil {
		respondError(w, http.StatusInternalServerError, "session creation failed")
		return
	}

	// Set httpOnly cookie — JS never sees tokens
	h.setSessionCookie(w, sess.ID, ttl)

	// Return ONLY user info
	respondJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"userId":   userInfo.UserID,
			"subject":  userInfo.Subject,
			"username": userInfo.Username,
			"email":    userInfo.Email,
		},
	})
}

// ── Kratos proxy ──

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

// ── Cookie helpers ──

func (h *BFFHandler) setSessionCookie(w http.ResponseWriter, id string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.SessionCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: parseSameSite(h.cfg.CookieSameSite),
		MaxAge:   int(ttl.Seconds()),
	})
}

func (h *BFFHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		MaxAge:   -1,
	})
}

// ── Session / Proxy API ──

// Me returns current user from session cookie.
func (h *BFFHandler) Me(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, err := h.store.Get(r.Context(), sessionID)
	if err != nil || sess == nil {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	respondJSON(w, http.StatusOK, sess.User)
}

// Logout deletes session and clears cookie.
func (h *BFFHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID != "" {
		h.store.Delete(r.Context(), sessionID)
	}
	h.clearSessionCookie(w)
	respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// Proxy forwards /api/* requests to iam-service with Bearer token.
func (h *BFFHandler) Proxy(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
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

	if sessionID != "" {
		sess, err := h.store.Get(r.Context(), sessionID)
		if err == nil && sess != nil {
			proxyReq.Header.Set("Authorization", "Bearer "+sess.AccessToken)
			proxyReq.Header.Set("X-User-Id", sess.User.UserID)
			proxyReq.Header.Set("X-User-Subject", sess.User.Subject)
			proxyReq.Header.Set("X-Username", sess.User.Username)
			proxyReq.Header.Set("X-User-Email", sess.User.Email)
			proxyReq.Header.Set("X-Auth-Checked", "true")
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

func (h *BFFHandler) putJSON(url string, body any) (*http.Response, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return h.httpClient.Do(req)
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
