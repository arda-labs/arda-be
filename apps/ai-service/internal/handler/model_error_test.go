package handler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
)

func TestModelErrorCodeMapsProviderStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unauthorized", &model.ProviderStatusError{StatusCode: 401}, "ai.model_unauthorized"},
		{"forbidden", &model.ProviderStatusError{StatusCode: 403}, "ai.model_unauthorized"},
		{"rate limited", &model.ProviderStatusError{StatusCode: 429}, "ai.model_rate_limited"},
		{"gateway timeout", &model.ProviderStatusError{StatusCode: 504}, "ai.model_timeout"},
		{"server error", &model.ProviderStatusError{StatusCode: 500}, "ai.model_unavailable"},
		{"deadline", context.DeadlineExceeded, "ai.model_timeout"},
		{"wrapped deadline", fmt.Errorf("stream: %w", context.DeadlineExceeded), "ai.model_timeout"},
		{"transport", errors.New("connection reset"), "ai.model_unavailable"},
		{"nil", nil, "ai.model_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelErrorCode(tc.err); got != tc.want {
				t.Fatalf("modelErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
