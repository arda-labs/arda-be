package http

import (
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/handler"
)

func TestNewRouterRegistersFinanceRoutes(t *testing.T) {
	t.Helper()
	NewRouter(handler.NewFinanceHandler(nil, nil, nil), handler.NewApprovalHandler(nil))
}
