package service_test

import (
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
