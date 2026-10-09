package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type quotaGateStore struct {
	agentRunStore
	startErr  error
	finalized []int64
}

func (s *quotaGateStore) Start(ctx context.Context, run repository.RunContext, message string) error {
	if s.startErr != nil {
		return s.startErr
	}
	return s.agentRunStore.Start(ctx, run, message)
}

func (s *quotaGateStore) ReserveQuota(context.Context, string, string, int64) error { return nil }

func (s *quotaGateStore) FinalizeQuota(_ context.Context, _, _ string, tokens int64) error {
	s.finalized = append(s.finalized, tokens)
	return nil
}

// A replayed run id shares the original run's reservation. Finalizing it with
// zero tokens would refund the allowance while the original run still spends.
func TestReplayedRunDoesNotFinalizeOriginalQuota(t *testing.T) {
	store := &quotaGateStore{startErr: repository.ErrRunAlreadyExists}
	router := NewRouterWithOptions(store, tools.NewRegistry(handlerTestTool{}), RouterOptions{})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"hello"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a replayed run, got %d", res.Code)
	}
	if len(store.finalized) != 0 {
		t.Fatalf("replay must not finalize the original reservation, got %v", store.finalized)
	}
}

// Termination on a cancelled context must leave quota finalization to the
// model loop, which knows the tokens already consumed.
func TestTerminateDoesNotRefundQuota(t *testing.T) {
	store := &quotaGateStore{}
	run := repository.RunContext{TenantID: "t1", ActorUserID: "u1", ExternalThread: "th", ExternalRun: "r"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if !terminateAgentRunOnContext(ctx, store, run, runInput{ThreadID: "th", RunID: "r"}, nil, "") {
		t.Fatal("expected the cancelled run to be terminalized")
	}
	if len(store.finalized) != 0 {
		t.Fatalf("terminate must not finalize quota with zero tokens, got %v", store.finalized)
	}
}
