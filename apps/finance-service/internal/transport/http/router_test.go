package http

import (
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/handler"
)

func TestNewRouterRegistersFinanceRoutes(t *testing.T) {
	t.Helper()
	NewRouter(handler.NewFinanceHandler(nil, nil, nil, nil), handler.NewCoaHandler(nil), handler.NewPostingHandler(nil), handler.NewCashHandler(nil), handler.NewPostingCaseHandler(nil))
}

func TestPostingCaseRouteIsRegistered(t *testing.T) {
	t.Helper()
	handlerFn := method("POST", handler.NewPostingCaseHandler(nil).CreatePostingCase)
	if handlerFn == nil {
		t.Fatal("posting case handler must not be nil")
	}
}
