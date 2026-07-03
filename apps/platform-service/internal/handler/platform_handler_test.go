package handler

import (
	"encoding/json"
	"testing"
)

func TestPublicBrandingPayloadStripsAuthSettings(t *testing.T) {
	payload := publicBrandingPayload(`{"appName":"Bank","loginLogoUrl":"/logo.svg","maxFailedAttempts":9}`)

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if got["appName"] != "Bank" || got["loginLogoUrl"] != "/logo.svg" {
		t.Fatalf("missing branding fields: %#v", got)
	}
	if _, ok := got["maxFailedAttempts"]; ok {
		t.Fatalf("leaked auth setting: %#v", got)
	}
}
