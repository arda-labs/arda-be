package service_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

func TestDeriveZeebeRestAddr(t *testing.T) {
	if got := service.DeriveZeebeRestAddr("192.168.1.10:26500"); got != "http://192.168.1.10:8080" {
		t.Fatalf("DeriveZeebeRestAddr() = %q", got)
	}
}

func TestDeriveZeebeTasklistAddr(t *testing.T) {
	got := service.DeriveZeebeTasklistAddr("http://zeebe-zeebe-gateway.platform.svc.cluster.local:8080")
	want := "http://zeebe-tasklist.platform.svc.cluster.local:8080"
	if got != want {
		t.Fatalf("DeriveZeebeTasklistAddr() = %q, want %q", got, want)
	}
}

func TestIsNativeUserTaskElement(t *testing.T) {
	if got := service.IsNativeUserTaskElement("UT_CheckerReview"); !got {
		t.Fatalf("expected UT_CheckerReview to be native user task element")
	}
	if got := service.IsNativeUserTaskElement("ST_Validate"); got {
		t.Fatalf("expected ST_Validate not to be native user task element")
	}
}

func TestZeebeRestGetVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/process-instances/123/variables" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"amount":500000000,"fundSource":"BRANCH"}`))
	}))
	defer srv.Close()

	client := service.NewZeebeRestClient(srv.URL, "", nil)
	vars, err := client.GetVariables(context.Background(), 123)
	if err != nil {
		t.Fatalf("GetVariables: %v", err)
	}
	if vars["fundSource"] != "BRANCH" {
		t.Fatalf("fundSource = %v, want BRANCH", vars["fundSource"])
	}
	if vars["amount"] != float64(500000000) {
		t.Fatalf("amount = %v, want 500000000", vars["amount"])
	}
}

func TestZeebeRestGetVariablesErrors(t *testing.T) {
	t.Run("unconfigured client", func(t *testing.T) {
		client := service.NewZeebeRestClient("", "", nil)
		if _, err := client.GetVariables(context.Background(), 123); err == nil {
			t.Fatal("expected error for unconfigured client")
		}
	})
	t.Run("upstream error surfaces status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()
		client := service.NewZeebeRestClient(srv.URL, "", nil)
		_, err := client.GetVariables(context.Background(), 123)
		if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
			t.Fatalf("expected upstream HTTP 500 error, got %v", err)
		}
	})
}
