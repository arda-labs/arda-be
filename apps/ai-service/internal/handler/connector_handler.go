package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type ConnectorDTO struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenantId,omitempty"`
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	TargetSource string `json:"targetSource"`
	SyncSchedule string `json:"syncSchedule"`
	Status       string `json:"status"`
	LastSyncAt   string `json:"lastSyncAt"`
	DocCount     int    `json:"docCount"`
	TotalChunks  int    `json:"totalChunks"`
}

type CreateConnectorRequest struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	TargetSource string `json:"targetSource"`
	SyncSchedule string `json:"syncSchedule"`
}

func toConnectorDTO(c repository.DataConnector) ConnectorDTO {
	return ConnectorDTO{
		ID:           c.ID,
		TenantID:     c.TenantID,
		Name:         c.Name,
		Provider:     c.Provider,
		TargetSource: c.TargetSource,
		SyncSchedule: c.SyncSchedule,
		Status:       c.Status,
		LastSyncAt:   c.LastSyncAt.UTC().Format(time.RFC3339),
		DocCount:     c.DocCount,
		TotalChunks:  c.TotalChunks,
	}
}

func handleConnectors(w http.ResponseWriter, r *http.Request, store runStore) {
	switch r.Method {
	case http.MethodGet:
		handleListConnectors(w, r, store)
	case http.MethodPost:
		handleCreateConnector(w, r, store)
	default:
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
	}
}

func handleListConnectors(w http.ResponseWriter, r *http.Request, store runStore) {
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	connStore, ok := store.(repository.ConnectorStore)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result": []ConnectorDTO{
				{
					ID:           "conn-gdrive-hr",
					Name:         "Google Drive - Sổ tay Nhân sự & Chính sách",
					Provider:     "google_drive",
					TargetSource: "Quy chế & Chế độ đãi ngộ 2026",
					SyncSchedule: "Hourly (Mỗi giờ)",
					Status:       "synced",
					LastSyncAt:   time.Now().UTC().Format(time.RFC3339),
					DocCount:     18,
					TotalChunks:  342,
				},
			},
		})
		return
	}

	connectors, err := connStore.ListConnectors(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.connectors_list_failed")
		return
	}

	dtos := make([]ConnectorDTO, 0, len(connectors))
	for _, c := range connectors {
		dtos = append(dtos, toConnectorDTO(c))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result":  dtos,
	})
}

func handleCreateConnector(w http.ResponseWriter, r *http.Request, store runStore) {
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}

	var req CreateConnectorRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		problem(w, http.StatusBadRequest, "ai.connector_name_required")
		return
	}
	targetSource := strings.TrimSpace(req.TargetSource)
	if targetSource == "" {
		problem(w, http.StatusBadRequest, "ai.connector_target_source_required")
		return
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "google_drive"
	}
	schedule := strings.TrimSpace(req.SyncSchedule)
	if schedule == "" {
		schedule = "Hourly"
	}

	connStore, ok := store.(repository.ConnectorStore)
	if !ok {
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"result": ConnectorDTO{
				ID:           "conn-mock-id",
				TenantID:     scope.TenantID,
				Name:         name,
				Provider:     provider,
				TargetSource: targetSource,
				SyncSchedule: schedule,
				Status:       "synced",
				LastSyncAt:   time.Now().UTC().Format(time.RFC3339),
				DocCount:     0,
				TotalChunks:  0,
			},
		})
		return
	}

	created, err := connStore.CreateConnector(r.Context(), repository.DataConnector{
		TenantID:     scope.TenantID,
		Name:         name,
		Provider:     provider,
		TargetSource: targetSource,
		SyncSchedule: schedule,
		Status:       "synced",
		LastSyncAt:   time.Now(),
		DocCount:     0,
		TotalChunks:  0,
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.connector_create_failed")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"result":  toConnectorDTO(*created),
	})
}

func handleConnectorSubtree(w http.ResponseWriter, r *http.Request, store runStore) {
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}

	subpath := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/rag/connectors/"), "/")
	if subpath == "" {
		problem(w, http.StatusBadRequest, "ai.connector_id_required")
		return
	}

	if strings.HasSuffix(subpath, "/sync") {
		if r.Method != http.MethodPost {
			problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
			return
		}
		connectorID := strings.TrimSuffix(subpath, "/sync")
		connectorID = strings.Trim(connectorID, "/")

		connStore, ok := store.(repository.ConnectorStore)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": true,
				"result": ConnectorDTO{
					ID:           connectorID,
					Name:         "Synced Connector",
					Status:       "synced",
					LastSyncAt:   time.Now().UTC().Format(time.RFC3339),
					DocCount:     1,
					TotalChunks:  12,
				},
			})
			return
		}

		synced, err := connStore.SyncConnector(r.Context(), scope.TenantID, connectorID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "ai.connector_sync_failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result":  toConnectorDTO(*synced),
		})
		return
	}

	if r.Method == http.MethodDelete {
		connectorID := subpath
		connStore, ok := store.(repository.ConnectorStore)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": true,
			})
			return
		}

		if err := connStore.DeleteConnector(r.Context(), scope.TenantID, connectorID); err != nil {
			problem(w, http.StatusInternalServerError, "ai.connector_delete_failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
		})
		return
	}

	problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
}
