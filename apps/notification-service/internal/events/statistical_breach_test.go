package events

import (
	"encoding/json"
	"testing"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
)

func TestStatisticalBreachInputMapsOwnerAndIdempotency(t *testing.T) {
	value := 12.5
	payload, err := json.Marshal(map[string]any{
		"alert_id":       "alert-1",
		"tenant_id":      "tenant-1",
		"rule_code":      "R1",
		"rule_name":      "Dư nợ xấu vượt ngưỡng",
		"owner_user_id":  "user-1",
		"indicator_code": "30020.02",
		"period_code":    "2026-09",
		"value":          value,
		"threshold":      3,
		"operator":       ">",
		"severity":       "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	in, err := StatisticalBreachInput(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.TenantID != "tenant-1" || in.IdempotencyKey != "statistical.indicator.breached:alert-1" {
		t.Fatalf("tenant/idempotency wrong: %+v", in)
	}
	if in.SourceService != "statistical-service" || in.SourceEventID != "alert-1" {
		t.Fatalf("source wrong: %+v", in)
	}
	if len(in.Recipients) != 1 || in.Recipients[0].UserID != "user-1" {
		t.Fatalf("recipient wrong: %+v", in.Recipients)
	}
	if len(in.Channels) != 1 || in.Channels[0] != domain.ChannelInApp {
		t.Fatalf("channels wrong: %+v", in.Channels)
	}
	if in.Type != "warning" || in.Priority != 1 {
		t.Fatalf("severity mapping wrong: type=%s priority=%d", in.Type, in.Priority)
	}
	if in.TemplateKey != "statistical.indicator.breached" {
		t.Fatalf("template key wrong: %s", in.TemplateKey)
	}
	if in.Params["ruleName"] != "Dư nợ xấu vượt ngưỡng" || in.Params["value"] != value {
		t.Fatalf("params wrong: %+v", in.Params)
	}
}

func TestStatisticalBreachInputRejectsUnaddressableEvents(t *testing.T) {
	cases := map[string]string{
		"missing tenant": `{"owner_user_id":"user-1"}`,
		"missing owner":  `{"tenant_id":"tenant-1"}`,
		"malformed":      `{`,
	}
	for name, payload := range cases {
		if _, err := StatisticalBreachInput([]byte(payload)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestStatisticalBreachInputFallsBackToCompositeEventID(t *testing.T) {
	payload := []byte(`{"tenant_id":"t","owner_user_id":"u","rule_code":"R","period_code":"2026-09","dimension_key":"P"}`)
	in, err := StatisticalBreachInput(payload)
	if err != nil {
		t.Fatal(err)
	}
	if in.IdempotencyKey != "statistical.indicator.breached:t|R|2026-09|P" {
		t.Fatalf("idempotency key = %s", in.IdempotencyKey)
	}
	if in.Type != "info" || in.Priority != 0 {
		t.Fatalf("default severity mapping wrong: type=%s priority=%d", in.Type, in.Priority)
	}
}
