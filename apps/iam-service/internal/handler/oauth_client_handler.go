package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/hydra"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// OAuthClientHandler manages Ory Hydra OAuth2 clients (X3).
type OAuthClientHandler struct {
	hydra *hydra.Client
}

func NewOAuthClientHandler(client *hydra.Client) *OAuthClientHandler {
	return &OAuthClientHandler{hydra: client}
}

func (h *OAuthClientHandler) unavailable(w http.ResponseWriter, r *http.Request) bool {
	if h.hydra == nil || !h.hydra.Enabled() {
		ardahttp.WriteProblem(w, r, http.StatusServiceUnavailable,
			ardaerrors.New(ardaerrors.CodeInternal, "hydra admin url is not configured"))
		return true
	}
	return false
}

// Clients handles GET /api/admin/oauth-clients and POST (create).
func (h *OAuthClientHandler) Clients(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		clients, err := h.hydra.ListClients(r.Context())
		if err != nil {
			writeHydraError(w, r, err)
			return
		}
		ardahttp.WriteEnvelopeUnpaged(w, r, clients)
	case http.MethodPost:
		payload := decodeClientBody(w, r)
		if payload == nil {
			return
		}
		if err := validateClient(payload); err != "" {
			ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err))
			return
		}
		created, err := h.hydra.CreateClient(r.Context(), payload)
		if err != nil {
			writeHydraError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
	default:
		writeMethodNotAllowedIam(w, r)
	}
}

// ClientByID handles GET/PUT/DELETE /api/admin/oauth-clients/{id}.
func (h *OAuthClientHandler) ClientByID(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w, r) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "client id is required"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		client, err := h.hydra.GetClient(r.Context(), id)
		if err != nil {
			writeHydraError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, client)
	case http.MethodPut:
		payload := decodeClientBody(w, r)
		if payload == nil {
			return
		}
		payload["client_id"] = id
		updated, err := h.hydra.UpdateClient(r.Context(), id, payload)
		if err != nil {
			writeHydraError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, updated)
	case http.MethodDelete:
		if err := h.hydra.DeleteClient(r.Context(), id); err != nil {
			writeHydraError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeMethodNotAllowedIam(w, r)
	}
}

func decodeClientBody(w http.ResponseWriter, r *http.Request) map[string]any {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return nil
	}
	return payload
}

func validateClient(payload map[string]any) string {
	name, _ := payload["client_name"].(string)
	if strings.TrimSpace(name) == "" {
		return "client_name is required"
	}
	redirects, _ := payload["redirect_uris"].([]any)
	if len(redirects) == 0 {
		return "redirect_uris is required"
	}
	return ""
}

func writeHydraError(w http.ResponseWriter, r *http.Request, err error) {
	ardahttp.WriteProblem(w, r, http.StatusBadGateway, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "hydra admin request failed", err))
}

func writeMethodNotAllowedIam(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}
