package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestCreatePostingCaseRejectsBadBody(t *testing.T) {
	// PostingService deps stay nil: the workflow-client guard fires before
	// any repository access.
	h := NewPostingCaseHandler(service.NewPostingCaseService(service.NewPostingService(nil, nil), nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/finance/posting-cases", strings.NewReader(`{not json`))
	req.Header.Set("X-Tenant-Id", "t1")
	h.CreatePostingCase(rec, req)
	if rec.Code != 400 {
		t.Fatalf("invalid JSON status = %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/finance/posting-cases", strings.NewReader(`{"flow":"SINGLE_ENTRY","posting_request":{"accounting_date":"2026-09-08","lines":[{"line_no":1,"direction":"DEBIT","amount_minor":100,"account_code":"1111"},{"line_no":2,"direction":"CREDIT","amount_minor":100,"account_code":"5111"}]}}`))
	req.Header.Set("X-Tenant-Id", "t1")
	h.CreatePostingCase(rec, req)
	if rec.Code != 500 {
		t.Fatalf("nil workflow client status = %d, want 500 (internal)", rec.Code)
	}
}

func TestCreatePostingCaseMethodAndTenantGuards(t *testing.T) {
	h := NewPostingCaseHandler(service.NewPostingCaseService(service.NewPostingService(nil, nil), nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/finance/posting-cases", nil)
	h.CreatePostingCase(rec, req)
	if rec.Code != 405 {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/finance/posting-cases", strings.NewReader(`{"flow":"SINGLE_ENTRY"}`))
	h.CreatePostingCase(rec, req)
	if rec.Code != 403 {
		t.Fatalf("missing tenant status = %d, want 403", rec.Code)
	}
}

func TestPostingRequestProtoJSONParsing(t *testing.T) {
	// The FE contract is snake_case; protojson must accept it directly.
	var req financev1.PostingRequest
	body := `{"accounting_date":"2026-09-08","currency_code":"VND","description":"Chi","lines":[{"line_no":1,"direction":"DEBIT","amount_minor":"100","account_code":"1111"},{"line_no":2,"direction":"CREDIT","amount_minor":"100","account_code":"5111"}]}`
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("protojson unmarshal error = %v", err)
	}
	if len(req.GetLines()) != 2 || req.GetLines()[0].GetAccountCode() != "1111" || req.GetLines()[1].GetAmountMinor() != 100 {
		t.Fatalf("parsed request mismatch: %+v", &req)
	}
}
