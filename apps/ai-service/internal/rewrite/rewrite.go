// Package rewrite turns a user question into alternative retrieval queries
// using the tenant's configured chat model. It is best-effort: callers fall
// back to the original query whenever the model is unavailable or returns
// unparseable output.
package rewrite

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

const systemPrompt = `Bạn viết lại câu hỏi tra cứu tài liệu thành tối đa 2 truy vấn tìm kiếm tiếng Việt ngắn gọn, giữ nguyên thực thể, số liệu và ý định. Ví dụ câu hỏi viết tắt hoặc mơ hồ cần được diễn giải đầy đủ. Chỉ trả về một mảng JSON gồm các chuỗi, không giải thích, không thêm văn bản khác.`

// Service rewrites queries with the tenant's active model configuration.
type Service struct {
	settings repository.TenantSettingsStore
	pool     *model.ClientPool
}

func New(settings repository.TenantSettingsStore, pool *model.ClientPool) *Service {
	return &Service{settings: settings, pool: pool}
}

// Rewrite returns alternative queries for the given question.
func (s *Service) Rewrite(ctx context.Context, tenantID, query string) ([]string, error) {
	if s == nil || s.settings == nil || s.pool == nil {
		return nil, nil
	}
	settings, err := s.settings.GetTenantSettings(ctx, tenantID)
	if err != nil || settings == nil || settings.BaseURL == "" || settings.ModelID == "" {
		return nil, err
	}

	provider := s.pool.GetProvider(tenantID, settings.BaseURL, settings.APIKey, settings.ModelID)
	var out strings.Builder
	_, _, err = provider.StreamChat(ctx, []model.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: query},
	}, nil, model.StreamCallbacks{
		OnTextDelta: func(delta string) { out.WriteString(delta) },
	})
	if err != nil {
		return nil, err
	}
	return parseVariants(out.String()), nil
}

func parseVariants(raw string) []string {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil
	}
	var variants []string
	if err := json.Unmarshal([]byte(raw[start:end+1]), &variants); err != nil {
		return nil
	}
	cleaned := make([]string, 0, len(variants))
	for _, variant := range variants {
		if variant = strings.TrimSpace(variant); variant != "" {
			cleaned = append(cleaned, variant)
		}
	}
	return cleaned
}
