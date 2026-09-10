package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/capital-service/internal/handler"
	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	"github.com/arda-labs/arda/apps/capital-service/internal/service"
)

type fakeCapitalService struct {
	movement     *repository.CapitalMovement
	amendContractID string
	amendReason     string
	detailID        string
	fundTypeID      string
}

func (f *fakeCapitalService) ListFundTypes(context.Context, string, bool) ([]repository.FundType, error) {
	return nil, nil
}

func (f *fakeCapitalService) CreateFundType(context.Context, string, string, *repository.FundType) (*repository.FundType, error) {
	return &repository.FundType{}, nil
}

func (f *fakeCapitalService) UpdateFundType(context.Context, string, string, *repository.FundType) (*repository.FundType, error) {
	return &repository.FundType{}, nil
}

func (f *fakeCapitalService) DeactivateFundType(_ context.Context, _, id string) error {
	f.fundTypeID = id
	return nil
}

func (f *fakeCapitalService) ListProducts(context.Context, string, bool) ([]repository.CapitalProduct, error) {
	return nil, nil
}

func (f *fakeCapitalService) UpsertProduct(context.Context, string, string, *repository.CapitalProduct) (*repository.CapitalProduct, error) {
	return &repository.CapitalProduct{}, nil
}

func (f *fakeCapitalService) ListContracts(context.Context, repository.ListContractsParams) ([]repository.CapitalContract, int, error) {
	return nil, 0, nil
}

func (f *fakeCapitalService) GetContractDetail(_ context.Context, _, id string) (*service.ContractDetail, error) {
	f.detailID = id
	return &service.ContractDetail{Contract: &repository.CapitalContract{ID: id}}, nil
}

func (f *fakeCapitalService) CreateContract(context.Context, string, string, *repository.CapitalContract) (*repository.CapitalContract, error) {
	return &repository.CapitalContract{}, nil
}

func (f *fakeCapitalService) SubmitAmendment(_ context.Context, _, _, contractID string, payload json.RawMessage, reason string) (*repository.ContractAmendment, error) {
	f.amendContractID = contractID
	f.amendReason = reason
	return &repository.ContractAmendment{ID: "adj-1", ContractID: contractID, Payload: payload, Reason: reason}, nil
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

func TestRouterDetailAndAmendmentRoutesCarryID(t *testing.T) {
	svc := &fakeCapitalService{}
	mux := NewRouter(handler.NewCapitalHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/capital/contracts/CTR-002", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || svc.detailID != "CTR-002" {
		t.Fatalf("detail status = %d id = %q, want 200 CTR-002", rec.Code, svc.detailID)
	}

	body := bytes.NewBufferString(`{"payload":{"amount_minor":2000},"reason":"tang von"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/capital/contracts/CTR-003/amendments", body)
	req2.Header.Set("X-Tenant-Id", "tenant-1")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated || svc.amendContractID != "CTR-003" {
		t.Fatalf("amendment status = %d id = %q, want 201 CTR-003", rec2.Code, svc.amendContractID)
	}
}

func TestRouterFundTypeDeleteCarriesID(t *testing.T) {
	svc := &fakeCapitalService{}
	mux := NewRouter(handler.NewCapitalHandler(svc))
	req := httptest.NewRequest(http.MethodDelete, "/api/capital/fund-types/FT-9", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || svc.fundTypeID != "FT-9" {
		t.Fatalf("status = %d id = %q, want 200 FT-9", rec.Code, svc.fundTypeID)
	}
}
