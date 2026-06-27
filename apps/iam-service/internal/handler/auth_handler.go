package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/auth"
)

const deviceCookieName = "arda_did"
const deviceCookieMaxAge = 365 * 24 * 60 * 60
const rememberMFACookieName = "arda_rmf"

// AuthHandler exposes authentication endpoints.
type AuthHandler struct {
	orch *auth.Orchestrator
	user *UserHandler
}

// NewAuthHandler creates an auth handler.
func NewAuthHandler(orch *auth.Orchestrator, user *UserHandler) *AuthHandler {
	return &AuthHandler{orch: orch, user: user}
}

// LoginPage returns provider list and hints for the login UI.
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	loginChallenge := r.URL.Query().Get("login_challenge")
	if loginChallenge == "" {
		respondError(w, http.StatusBadRequest, "missing login_challenge")
		return
	}

	data, err := h.orch.GetLoginPageData(r.Context(), loginChallenge)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, data)
}

// LoginPassword handles username/password login.
func (h *AuthHandler) LoginPassword(w http.ResponseWriter, r *http.Request) {
	var req auth.PasswordLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	clientIP := extractIP(r)
	userAgent := r.UserAgent()
	requestID := r.Header.Get("X-Request-ID")
	deviceToken := ensureDeviceToken(w, r)

	result, err := h.orch.LoginWithPassword(r.Context(), &req, clientIP, userAgent, requestID, deviceToken)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// LoginMFA completes password login after a successful MFA challenge.
func (h *AuthHandler) LoginMFA(w http.ResponseWriter, r *http.Request) {
	var req auth.MFALoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.orch.LoginWithMFA(r.Context(), &req)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if req.RememberDevice {
		setShortLivedCookie(w, r, rememberMFACookieName, "1", 10*60)
	} else {
		clearShortLivedCookie(w, r, rememberMFACookieName)
	}

	respondJSON(w, http.StatusOK, result)
}

// LoginExternal initiates an external SSO login.
func (h *AuthHandler) LoginExternal(w http.ResponseWriter, r *http.Request) {
	var req auth.ExternalLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.orch.InitiateExternalLogin(r.Context(), &req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// CallbackProvider handles the callback from an external IdP (OIDC/SAML).
func (h *AuthHandler) CallbackProvider(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider_id")
	if providerID == "" {
		respondError(w, http.StatusBadRequest, "missing provider_id")
		return
	}

	params := make(map[string]string)
	for key, vals := range r.URL.Query() {
		if len(vals) > 0 {
			params[key] = vals[0]
		}
	}

	result, err := h.orch.HandleExternalCallback(r.Context(), providerID, params)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	http.Redirect(w, r, result.RedirectURL, http.StatusFound)
}

// CallbackToken exchanges the authorization code for tokens.
func (h *AuthHandler) CallbackToken(w http.ResponseWriter, r *http.Request) {
	var req auth.TokenExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.orch.ExchangeCode(r.Context(), &req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// Refresh handles token refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "missing refresh_token")
		return
	}

	result, err := h.orch.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// ListProviders returns all enabled providers.
func (h *AuthHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	p := h.orch.ListProviders()
	respondJSON(w, http.StatusOK, map[string]any{"providers": p})
}

// Consent auto-accepts consent for internal trusted clients.
func (h *AuthHandler) Consent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConsentChallenge string `json:"consent_challenge"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ConsentChallenge == "" {
		respondError(w, http.StatusBadRequest, "missing consent_challenge")
		return
	}

	redirectTo, err := h.orch.HandleConsent(r.Context(), req.ConsentChallenge)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"redirect_url": redirectTo})
}

// RegisterUser creates a new internal user with bcrypt password.
func (h *AuthHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "username and password required")
		return
	}

	user, err := h.orch.CreateUser(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"id":       user.ID,
		"username": user.Username,
		"email":    user.Email,
	})
}

func readDeviceToken(r *http.Request) string {
	cookie, err := r.Cookie(deviceCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func ensureDeviceToken(w http.ResponseWriter, r *http.Request) string {
	if token := readDeviceToken(r); token != "" {
		return token
	}

	token := generateDeviceToken()
	http.SetCookie(w, &http.Cookie{
		Name:     deviceCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   deviceCookieMaxAge,
	})
	return token
}

func generateDeviceToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setShortLivedCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func clearShortLivedCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
