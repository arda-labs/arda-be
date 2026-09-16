package catalog

import (
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func TestResultNamespaceScopesToConversationAndActor(t *testing.T) {
	scope := tools.Context{TenantID: "t1", ActorUserID: "u1", RequestID: "req-1", ExternalThread: "th-1"}
	if got := resultNamespace(scope); got != "t1|u1|th-1" {
		t.Fatalf("thread scope = %q", got)
	}

	scope.ExternalThread = ""
	if got := resultNamespace(scope); got != "t1|u1|req-1" {
		t.Fatalf("request fallback = %q", got)
	}

	scope.RequestID = ""
	if got := resultNamespace(scope); got != "t1|u1|anonymous" {
		t.Fatalf("anonymous fallback = %q", got)
	}

	other := tools.Context{TenantID: "t1", ActorUserID: "u2", ExternalThread: "th-1"}
	if resultNamespace(scope) == resultNamespace(other) {
		t.Fatal("another actor must never share a result namespace")
	}
}
