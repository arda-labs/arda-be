package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
)

type fakeSettlement struct {
	settleCode string
}

func (f *fakeSettlement) Open(context.Context, string, *service.OpenSavingsInput) (*repository.Savings, error) {
	return &repository.Savings{}, nil
}

func (f *fakeSettlement) SubmitSettle(_ context.Context, _, _, code string) (*service.Submission, error) {
	f.settleCode = code
	return &service.Submission{CaseID: "case-1", CaseCode: "DPM-SETTLE-1"}, nil
}

func (f *fakeSettlement) ListSavings(context.Context, string, []string, string, string) ([]repository.Savings, error) {
	return nil, nil
}

func (f *fakeSettlement) ListProducts(context.Context, repository.ListProductsParams) ([]repository.SavingsProduct, error) {
	return nil, nil
}

func (f *fakeSettlement) UpsertProduct(context.Context, string, string, *repository.SavingsProduct) (*repository.SavingsProduct, error) {
	return &repository.SavingsProduct{}, nil
}

func (f *fakeSettlement) ListInterbank(context.Context, string, []string, string) ([]repository.InterbankDeposit, error) {
	return nil, nil
}

type fakeAdditional struct {
	code   string
	amount int64
}

func (f *fakeAdditional) Submit(_ context.Context, _, _, code string, amountMinor int64, _ string) (*service.Submission, error) {
	f.code = code
	f.amount = amountMinor
	return &service.Submission{CaseID: "case-2", CaseCode: "DPM-ADD-1"}, nil
}

type fakeProducts struct{}

func (f *fakeProducts) Submit(context.Context, string, string, service.ProductRequestInput) (*service.Submission, error) {
	return &service.Submission{CaseID: "case-3", CaseCode: "DPM-PROD-1"}, nil
}

func (f *fakeProducts) List(context.Context, string, string) ([]repository.ProductRequest, error) {
	return nil, nil
}

func TestRouterSettleRouteCarriesCode(t *testing.T) {
	svc := &fakeSettlement{}
	mux := NewRouter(handler.NewDepositHandler(svc, &fakeAdditional{}, &fakeProducts{}))

	req := httptest.NewRequest(http.MethodPost, "/api/deposit/savings/SAV-001/settle", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.settleCode != "SAV-001" {
		t.Fatalf("service received code %q, want SAV-001", svc.settleCode)
	}
}

func TestRouterAdditionalDepositRouteCarriesCode(t *testing.T) {
	additional := &fakeAdditional{}
	mux := NewRouter(handler.NewDepositHandler(&fakeSettlement{}, additional, &fakeProducts{}))

	body := bytes.NewBufferString(`{"amount_minor":250000,"txn_date":"2026-09-11"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/deposit/savings/SAV-002/deposit", body)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if additional.code != "SAV-002" || additional.amount != 250000 {
		t.Fatalf("service received (%q, %d), want (SAV-002, 250000)", additional.code, additional.amount)
	}
}

func TestRouterRejectsWrongMethodOnActionRoutes(t *testing.T) {
	mux := NewRouter(handler.NewDepositHandler(&fakeSettlement{}, &fakeAdditional{}, &fakeProducts{}))
	req := httptest.NewRequest(http.MethodGet, "/api/deposit/savings/SAV-001/settle", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
