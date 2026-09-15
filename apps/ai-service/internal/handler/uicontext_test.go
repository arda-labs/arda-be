package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func TestUIContextFromForwardedProps(t *testing.T) {
	good := json.RawMessage(`{"ardaContext":{"currentScreen":"/customers/123","activeCustomerId":"c1"}}`)
	got := uiContextFromForwardedProps(good)
	if !strings.Contains(got, `"activeCustomerId":"c1"`) || !strings.Contains(got, "/customers/123") {
		t.Fatalf("context = %q, want both keys", got)
	}

	if got := uiContextFromForwardedProps(nil); got != "" {
		t.Fatalf("nil forwardedProps must yield empty context, got %q", got)
	}
	if got := uiContextFromForwardedProps(json.RawMessage(`{"runConfig":{"x":1}}`)); got != "" {
		t.Fatalf("missing ardaContext must yield empty context, got %q", got)
	}
	if got := uiContextFromForwardedProps(json.RawMessage(`{"ardaContext":"not-an-object"}`)); got != "" {
		t.Fatalf("non-object ardaContext must yield empty context, got %q", got)
	}
	oversized := json.RawMessage(`{"ardaContext":{"pad":"` + strings.Repeat("x", uiContextLimit+1) + `"}}`)
	if got := uiContextFromForwardedProps(oversized); got != "" {
		t.Fatalf("oversized ardaContext must be discarded, got %d bytes", len(got))
	}
	oversizedRaw := json.RawMessage(`{"ardaContext":{},"pad":"` + strings.Repeat("x", uiContextRawLimit+1) + `"}`)
	if got := uiContextFromForwardedProps(oversizedRaw); got != "" {
		t.Fatalf("oversized forwardedProps must be discarded, got %d bytes", len(got))
	}
}

func TestBuildModelMessagesInjectsBoundedUIContext(t *testing.T) {
	messages := buildModelMessages(
		context.Background(),
		nil,
		RouterOptions{},
		tools.Context{TenantID: "tenant-1", ActorUserID: "user-1"},
		repository.RunContext{},
		"khách này thế nào",
		`{"currentScreen":"/customers/123","activeCustomerId":"c1"}`,
	)

	var uiMessage string
	for _, message := range messages {
		if strings.Contains(message.Content, "Client UI context") {
			uiMessage = message.Content
		}
	}
	if uiMessage == "" {
		t.Fatalf("UI context message missing: %+v", messages)
	}
	if !strings.Contains(uiMessage, "untrusted") {
		t.Fatalf("UI context must be framed as untrusted: %q", uiMessage)
	}
	if !strings.Contains(uiMessage, `"activeCustomerId":"c1"`) {
		t.Fatalf("UI context payload missing: %q", uiMessage)
	}

	withoutContext := buildModelMessages(
		context.Background(),
		nil,
		RouterOptions{},
		tools.Context{TenantID: "tenant-1", ActorUserID: "user-1"},
		repository.RunContext{},
		"xin chào",
		"",
	)
	for _, message := range withoutContext {
		if strings.Contains(message.Content, "Client UI context") {
			t.Fatalf("empty UI context must inject nothing: %+v", withoutContext)
		}
	}
}
