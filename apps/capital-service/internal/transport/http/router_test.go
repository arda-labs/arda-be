package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/capital-service/internal/handler"
	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
)

type fakeCapitalService struct {
	movement *repository.CapitalMovement
}

func (f *fakeCapitalService) ListFundTypes(context.Context, string) ([]repository.FundType, error) {
	return nil, nil
}

func (f *fakeCapitalService) ListContracts(context.Context, repository.ListContractsParams) ([]repository.CapitalContract, int, error) {
	return nil, 0, nil
}

func (f *fakeCapitalService) CreateContract(context.Context, string, string, *repository.CapitalContract) (*repository.CapitalContract, error) {
	return &repository.CapitalContract{}, nil
}

func (f *fakeCapitalService) RecordMovement(_ context.Context, _, _ string, in *repository.CapitalMovement) (*repository.CapitalMovement, error) {
	f.movement = in
	return in, nil
}

func TestRouterMovementRouteCarriesContractID(t *testing.T) {
	svc := &fakeCapitalService{}
	mux := NewRouter(handler.NewCapitalHandler(svc))

	body := bytes.NewBufferString(`{"movement_type":"RECEIPT","amount_minor":1000,"movement_date":"2026-09-11"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/capital/contracts/CTR-001/movements", body)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.movement == nil || svc.movement.ContractID != "CTR-001" {
		t.Fatalf("service received contract id %v, want CTR-001", svc.movement)
	}
	if svc.movement.AmountMinor != 1000 {
		t.Fatalf("service received amount %d, want 1000", svc.movement.AmountMinor)
	}
}
