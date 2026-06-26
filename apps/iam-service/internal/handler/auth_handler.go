package handler

import (
	"encoding/json"
	"net/http"

	"github.com/arda-labs/arda/apps/iam-service/internal/auth"
)

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

	result, err := h.orch.LoginWithPassword(r.Context(), &req, clientIP, userAgent, requestID)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
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
