package http

// capital-service gọi workflow-service (và finance-service) bằng gRPC từ handler HTTP; tenant/user của BFF phải
// nằm trong metadata gửi đi (ardametadata.HTTPMiddleware), nếu không workflow từ chối "verified tenant scope is required".

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/capital-service/internal/handler"
	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

type scopeRecordingCapital struct {
	fakeCapitalService
	outgoing ardametadata.Context
}

func (f *scopeRecordingCapital) RecordMovement(ctx context.Context, tenantID, actor string, in *repository.CapitalMovement) (*repository.CapitalMovement, error) {
	f.outgoing = ardametadata.FromOutgoing(ctx)
	return f.fakeCapitalService.RecordMovement(ctx, tenantID, actor, in)
}

func TestRouterBindsGatewayIdentityToOutgoingMetadata(t *testing.T) {
	svc := &scopeRecordingCapital{}
	mux := NewRouter(handler.NewCapitalHandler(svc), handler.NewInternalAIHandler(&svc.fakeCapitalService), nil)
	body := bytes.NewBufferString(`{"movement_type":"RECEIPT","amount_minor":1000,"movement_date":"2026-09-11"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/capital/contracts/CTR-001/movements", body)
	req.Header.Set("X-Tenant-Id", "system")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Auth-Checked", "true")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	got := svc.outgoing
	if got.TenantID != "system" || got.UserID != "user-1" || got.AuthChecked != "true" {
		t.Fatalf("outgoing metadata = %+v, want tenant system, user user-1, auth-checked true", got)
	}
}
