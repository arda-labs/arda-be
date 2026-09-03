package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// fakeRAGFeedbacker implements ragFeedbacker for handler tests.
type fakeRAGFeedbacker struct {
	recordedRunID  string
	recordedHelpful bool
	recordedComment string
	recordedTenant  string
	recordedUser    string
	returnErr      error
}

func (f *fakeRAGFeedbacker) Feedback(_ context.Context, md metadata.Context, runID string, helpful bool, comment string) (*svcclient.FeedbackOut, error) {
	f.recordedRunID = runID
	f.recordedHelpful = helpful
	f.recordedComment = comment
	f.recordedTenant = md.TenantID
	f.recordedUser = md.UserID
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return &svcclient.FeedbackOut{
		ID:        "fb-1",
		RunID:     runID,
		Helpful:   helpful,
		Comment:   comment,
		CreatedAt: "2026-09-03T12:00:00Z",
	}, nil
}

func TestFeedbackAuthRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ai/feedback",
		strings.NewReader(`{"run_id":"r1","helpful":true}`))
	res := httptest.NewRecorder()
	NewRouterWithOptions(nil, nil, RouterOptions{RAGClient: &fakeRAGFeedbacker{}}).ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
	var body map[string]any
	json.NewDecoder(res.Body).Decode(&body)
	if code, _ := body["code"]; code != "ai.auth_required" {
		t.Errorf("code = %v, want ai.auth_required", code)
	}
}

func TestFeedbackForbiddenWithoutAssistantPermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ai/feedback",
		strings.NewReader(`{"run_id":"r1","helpful":true}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "u-1")
	req.Header.Set("X-Tenant-Id", "t-1")
	req.Header.Set("X-Permissions", "crm.customer.read")
	res := httptest.NewRecorder()
	NewRouterWithOptions(nil, nil, RouterOptions{RAGClient: &fakeRAGFeedbacker{}}).ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusForbidden)
	}
	var body map[string]any
	json.NewDecoder(res.Body).Decode(&body)
	if code, _ := body["code"]; code != "ai.assistant_forbidden" {
		t.Errorf("code = %v, want ai.assistant_forbidden", code)
	}
}

func TestFeedbackValidRequest(t *testing.T) {
	fake := &fakeRAGFeedbacker{}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/feedback",
		strings.NewReader(`{"run_id":"r1","helpful":true,"comment":"Great answer"}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "u-1")
	req.Header.Set("X-Tenant-Id", "t-1")
	req.Header.Set("X-Permissions", "ai.assistant.use")
	res := httptest.NewRecorder()
	NewRouterWithOptions(nil, nil, RouterOptions{RAGClient: fake}).ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusCreated)
	}
	var out svcclient.FeedbackOut
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.ID != "fb-1" || out.RunID != "r1" || out.Helpful != true || out.Comment != "Great answer" {
		t.Errorf("FeedbackOut = %+v", out)
	}
	if fake.recordedRunID != "r1" || fake.recordedHelpful != true || fake.recordedComment != "Great answer" {
		t.Errorf("fake record = %s/%v/%s", fake.recordedRunID, fake.recordedHelpful, fake.recordedComment)
	}
	if fake.recordedTenant != "t-1" || fake.recordedUser != "u-1" {
		t.Errorf("fake headers = tenant=%q user=%q", fake.recordedTenant, fake.recordedUser)
	}
}

func TestFeedbackNotFound(t *testing.T) {
	fake := &fakeRAGFeedbacker{returnErr: &svcclient.StatusError{Service: "rag-service", Status: http.StatusNotFound}}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/feedback",
		strings.NewReader(`{"run_id":"r1","helpful":true}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "u-1")
	req.Header.Set("X-Tenant-Id", "t-1")
	req.Header.Set("X-Permissions", "ai.assistant.use")
	res := httptest.NewRecorder()
	NewRouterWithOptions(nil, nil, RouterOptions{RAGClient: fake}).ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
	var body map[string]any
	json.NewDecoder(res.Body).Decode(&body)
	if code, _ := body["code"]; code != "ai.feedback_run_not_found" {
		t.Errorf("code = %v, want ai.feedback_run_not_found", code)
	}
}

func TestFeedbackNilRAGClient(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ai/feedback",
		strings.NewReader(`{"run_id":"r1","helpful":true}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "u-1")
	req.Header.Set("X-Tenant-Id", "t-1")
	req.Header.Set("X-Permissions", "ai.assistant.use")
	res := httptest.NewRecorder()
	NewRouterWithOptions(nil, nil, RouterOptions{}).ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
	var body map[string]any
	json.NewDecoder(res.Body).Decode(&body)
	if code, _ := body["code"]; code != "ai.feedback_unavailable" {
		t.Errorf("code = %v, want ai.feedback_unavailable", code)
	}
}

func TestFeedbackMalformedBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ai/feedback",
		strings.NewReader(`{"run_id":"r1","helpful":true,"extra":"field"}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "u-1")
	req.Header.Set("X-Tenant-Id", "t-1")
	req.Header.Set("X-Permissions", "ai.assistant.use")
	res := httptest.NewRecorder()
	NewRouterWithOptions(nil, nil, RouterOptions{RAGClient: &fakeRAGFeedbacker{}}).ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusBadRequest)
	}
	var body map[string]any
	json.NewDecoder(res.Body).Decode(&body)
	if code, _ := body["code"]; code != "ai.invalid_feedback_input" {
		t.Errorf("code = %v, want ai.invalid_feedback_input", code)
	}
}