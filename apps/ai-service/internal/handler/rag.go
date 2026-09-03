package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

type RAGHandler struct {
	svc *knowledge.Service
}

func NewRAGHandler(svc *knowledge.Service) *RAGHandler {
	return &RAGHandler{svc: svc}
}

func (h *RAGHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/rag/query", h.handleQuery)
	mux.HandleFunc("/api/rag/feedback", h.handleFeedback)
	mux.HandleFunc("/api/rag/sources", h.handleSourcesRoot)
	mux.HandleFunc("/api/rag/sources/", h.handleSourcesSubtree)
	mux.HandleFunc("/api/rag/jobs/", h.handleJobs)
}

func (h *RAGHandler) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "rag.method_not_allowed")
		return
	}

	scope := scopeFromRequest(r)
	var req knowledge.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "rag.invalid_json")
		return
	}

	res, err := h.svc.Query(r.Context(), req, scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "rag.query_failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *RAGHandler) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "rag.method_not_allowed")
		return
	}

	var req knowledge.FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "rag.invalid_json")
		return
	}

	res, err := h.svc.Repo().SaveFeedback(r.Context(), req.RunID, req.Helpful, req.Comment)
	if err != nil {
		problem(w, http.StatusInternalServerError, "rag.feedback_failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *RAGHandler) handleSourcesRoot(w http.ResponseWriter, r *http.Request) {
	scope := scopeFromRequest(r)
	switch r.Method {
	case http.MethodGet:
		includeDeleted := r.URL.Query().Get("include_deleted") == "true"
		sources, err := h.svc.Repo().ListSources(r.Context(), includeDeleted)
		if err != nil {
			problem(w, http.StatusInternalServerError, "rag.list_sources_failed")
			return
		}
		if sources == nil {
			sources = []knowledge.Source{}
		}
		writeJSON(w, http.StatusOK, sources)

	case http.MethodPost:
		var data knowledge.SourceCreate
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			problem(w, http.StatusBadRequest, "rag.invalid_json")
			return
		}
		src, err := h.svc.Repo().CreateSource(r.Context(), data, scope.TenantID, scope.ActorUserID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "rag.create_source_failed")
			return
		}
		writeJSON(w, http.StatusCreated, src)

	default:
		problem(w, http.StatusMethodNotAllowed, "rag.method_not_allowed")
	}
}

func (h *RAGHandler) handleSourcesSubtree(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/rag/sources/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// Special endpoints: preview-chunks & parse-preview
	if len(parts) == 1 && parts[0] == "preview-chunks" && r.Method == http.MethodPost {
		var req knowledge.ChunkPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			problem(w, http.StatusBadRequest, "rag.invalid_json")
			return
		}
		res, err := h.svc.PreviewChunks(req)
		if err != nil {
			problem(w, http.StatusBadRequest, "rag.preview_failed")
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	if len(parts) == 1 && parts[0] == "parse-preview" && r.Method == http.MethodPost {
		err := r.ParseMultipartForm(32 << 20) // 32MB max
		if err != nil {
			problem(w, http.StatusBadRequest, "rag.invalid_form")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			problem(w, http.StatusBadRequest, "rag.missing_file")
			return
		}
		defer file.Close()

		fileBytes, err := io.ReadAll(file)
		if err != nil {
			problem(w, http.StatusBadRequest, "rag.read_file_failed")
			return
		}

		chunkSize, _ := strconv.Atoi(r.FormValue("chunk_size"))
		chunkOverlap, _ := strconv.Atoi(r.FormValue("chunk_overlap"))

		res, err := h.svc.ParseAndPreviewFile(fileBytes, header.Filename, chunkSize, chunkOverlap)
		if err != nil {
			problem(w, http.StatusBadRequest, "rag.parse_preview_failed")
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	// /api/rag/sources/{source_id}
	if len(parts) == 1 {
		sourceID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			problem(w, http.StatusBadRequest, "rag.invalid_source_id")
			return
		}
		if r.Method == http.MethodGet {
			src, err := h.svc.Repo().GetSource(r.Context(), sourceID)
			if err != nil {
				problem(w, http.StatusNotFound, "rag.source_not_found")
				return
			}
			writeJSON(w, http.StatusOK, src)
			return
		}
		if r.Method == http.MethodDelete {
			err := h.svc.Repo().SoftDeleteSource(r.Context(), sourceID)
			if err != nil {
				problem(w, http.StatusNotFound, "rag.source_not_found")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		problem(w, http.StatusMethodNotAllowed, "rag.method_not_allowed")
		return
	}

	// /api/rag/sources/{source_id}/versions
	if len(parts) == 2 && parts[1] == "versions" {
		sourceID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			problem(w, http.StatusBadRequest, "rag.invalid_source_id")
			return
		}
		scope := scopeFromRequest(r)
		if r.Method == http.MethodGet {
			versions, err := h.svc.Repo().ListVersions(r.Context(), sourceID)
			if err != nil {
				problem(w, http.StatusInternalServerError, "rag.list_versions_failed")
				return
			}
			if versions == nil {
				versions = []knowledge.Version{}
			}
			writeJSON(w, http.StatusOK, versions)
			return
		}
		if r.Method == http.MethodPost {
			var data knowledge.VersionCreate
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				problem(w, http.StatusBadRequest, "rag.invalid_json")
				return
			}
			v, err := h.svc.Repo().CreateVersion(r.Context(), sourceID, data, scope.ActorUserID)
			if err != nil {
				problem(w, http.StatusInternalServerError, "rag.create_version_failed")
				return
			}
			writeJSON(w, http.StatusCreated, v)
			return
		}
		problem(w, http.StatusMethodNotAllowed, "rag.method_not_allowed")
		return
	}

	// /api/rag/sources/{source_id}/versions/{version_id}
	if len(parts) == 3 && parts[1] == "versions" {
		sourceID, _ := strconv.ParseInt(parts[0], 10, 64)
		versionID, _ := strconv.ParseInt(parts[2], 10, 64)
		if r.Method == http.MethodGet {
			v, err := h.svc.Repo().GetVersion(r.Context(), sourceID, versionID)
			if err != nil {
				problem(w, http.StatusNotFound, "rag.version_not_found")
				return
			}
			writeJSON(w, http.StatusOK, v)
			return
		}
		problem(w, http.StatusMethodNotAllowed, "rag.method_not_allowed")
		return
	}

	// /api/rag/sources/{source_id}/versions/{version_id}/review
	if len(parts) == 4 && parts[1] == "versions" && parts[3] == "review" && r.Method == http.MethodPost {
		sourceID, _ := strconv.ParseInt(parts[0], 10, 64)
		versionID, _ := strconv.ParseInt(parts[2], 10, 64)
		scope := scopeFromRequest(r)

		var req knowledge.ReviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			problem(w, http.StatusBadRequest, "rag.invalid_json")
			return
		}
		v, err := h.svc.Repo().ReviewVersion(r.Context(), sourceID, versionID, req, scope.ActorUserID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "rag.review_failed")
			return
		}
		writeJSON(w, http.StatusOK, v)
		return
	}

	// /api/rag/sources/{source_id}/versions/{version_id}/publish
	if len(parts) == 4 && parts[1] == "versions" && parts[3] == "publish" && r.Method == http.MethodPost {
		sourceID, _ := strconv.ParseInt(parts[0], 10, 64)
		versionID, _ := strconv.ParseInt(parts[2], 10, 64)
		scope := scopeFromRequest(r)

		res, err := h.svc.Repo().PublishVersion(r.Context(), sourceID, versionID, scope.ActorUserID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "rag.publish_failed")
			return
		}
		writeJSON(w, http.StatusAccepted, res)
		return
	}

	problem(w, http.StatusNotFound, "rag.not_found")
}

func (h *RAGHandler) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "rag.method_not_allowed")
		return
	}
	jobID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/rag/jobs/"), "/")
	if jobID == "" {
		problem(w, http.StatusBadRequest, "rag.invalid_job_id")
		return
	}
	job, err := h.svc.Repo().GetJob(r.Context(), jobID)
	if err != nil {
		problem(w, http.StatusNotFound, "rag.job_not_found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
