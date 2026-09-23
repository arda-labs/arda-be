package handler

import (
	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"net/http"
)

func validateChatModels(w http.ResponseWriter, ids []string) bool {
	for _, id := range ids {
		if model.IsDecisionModelID(id) {
			problem(w, http.StatusBadRequest, "ai.model_purpose_mismatch")
			return false
		}
	}
	return true
}
