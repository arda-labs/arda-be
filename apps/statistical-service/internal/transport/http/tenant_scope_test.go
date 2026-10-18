package http

// statistical-service gọi workflow-service bằng gRPC từ handler HTTP; tenant/user của BFF phải nằm trong metadata
// gửi đi (ardametadata.HTTPMiddleware), nếu không workflow từ chối "verified tenant scope is required".

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/statistical-service/internal/handler"
	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

type scopeRecordingStatisticalSource struct {
	stubStatisticalAISource
	outgoing ardametadata.Context
}

func (s *scopeRecordingStatisticalSource) ListReportDefinitions(ctx context.Context, params repository.ListReportDefinitionsParams) ([]repository.ReportDefinition, error) {
	s.outgoing = ardametadata.FromOutgoing(ctx)
	return s.stubStatisticalAISource.ListReportDefinitions(ctx, params)
}

func TestRouterBindsGatewayIdentityToOutgoingMetadata(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := &scopeRecordingStatisticalSource{}
	router := NewRouter(handler.NewStatisticalHandler(nil), handler.NewInternalAIHandler(source), nil, nil, nil)
	token, err := identity.Issue(aiTestSecret, "ai-service", "statistical-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/ai/report-definitions", nil)
	req.Header.Set("X-Service-Auth", token)
	req.Header.Set("X-Tenant-Id", "system")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if source.outgoing.TenantID != "system" {
		t.Fatalf("outgoing metadata = %+v, want tenant system", source.outgoing)
	}
}
