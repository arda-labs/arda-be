package http

// loan-service gọi workflow-service / finance-service / platform-service bằng gRPC từ handler HTTP; tenant/user của
// BFF phải nằm trong metadata gửi đi (ardametadata.HTTPMiddleware), nếu không workflow từ chối
// "verified tenant scope is required" (gateway trả 502).

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

type scopeRecordingLoanSource struct {
	stubReportingLoanSource
	outgoing ardametadata.Context
}

func (s *scopeRecordingLoanSource) ListAgreementsForReporting(ctx context.Context, tenantID, orgCode string) ([]domain.ReportingAgreement, error) {
	s.outgoing = ardametadata.FromOutgoing(ctx)
	return s.stubReportingLoanSource.ListAgreementsForReporting(ctx, tenantID, orgCode)
}

func TestRouterBindsGatewayIdentityToOutgoingMetadata(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := &scopeRecordingLoanSource{}
	router := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, handler.NewInternalReportingHandler(source), nil, nil)
	token, err := identity.Issue(aiTestSecret, "statistical-service", "loan-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	res := reportingSignedRequest(t, router, "/internal/reporting/loan-agreements", "system", token)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", res.Code, res.Body.String())
	}
	if source.outgoing.TenantID != "system" {
		t.Fatalf("outgoing metadata = %+v, want tenant system", source.outgoing)
	}
}
