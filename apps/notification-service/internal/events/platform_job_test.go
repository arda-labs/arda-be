package events

import (
	"encoding/json"
	"testing"
)

func TestPlatformJobInputMapsFields(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"event_id": "run-1:platform.job.failed", "job_code": "ai-eval", "job_name": "AI retrieval eval",
		"run_id": "run-1", "status": "FAILED", "error_message": "12/12 case failed", "tenant_id": "tenant-a",
		"user_ids": []string{"u1"}, "href": "/admin/jobs?tab=scheduled&run=run-1",
	})
	in, err := PlatformJobInput(payload, PlatformJobFailedSubject, "vi-VN")
	if err != nil {
		t.Fatal(err)
	}
	if in.TenantID != "tenant-a" || in.EventType != "platform.job.failed.v1" || in.TitleKey != "platform.job.failed.title" ||
		in.IdempotencyKey != "platform.job.failed.v1:run-1:platform.job.failed" || in.Href != "/admin/jobs?tab=scheduled&run=run-1" ||
		in.EntityID != "run-1" || in.EntityType != "scheduled_job_run" || in.SourceService != "platform-service" ||
		in.Locale != "vi-VN" || in.Type != "error" {
		t.Fatalf("unexpected input %+v", in)
	}
	if in.Params["jobName"] != "AI retrieval eval" || in.Params["errorMessage"] != "12/12 case failed" || in.Params["jobCode"] != "ai-eval" {
		t.Fatalf("params missing: %v", in.Params)
	}
	if len(in.Recipients) != 1 || in.Recipients[0].UserID != "u1" || in.Recipients[0].Type != "user" {
		t.Fatalf("recipients: %+v", in.Recipients)
	}
}

func TestPlatformJobInputRecoveredIsSuccess(t *testing.T) {
	payload := []byte(`{"event_id":"run-2:platform.job.recovered","run_id":"run-2","tenant_id":"t","status":"SUCCEEDED","user_ids":["u1"],"locale":"en-US"}`)
	in, err := PlatformJobInput(payload, PlatformJobRecoveredSubject, "vi-VN")
	if err != nil {
		t.Fatal(err)
	}
	if in.Type != "success" || in.EventType != "platform.job.recovered.v1" || in.Locale != "en-US" {
		t.Fatalf("unexpected input %+v", in)
	}
}

func TestPlatformJobInputRejectsUnaddressable(t *testing.T) {
	for name, p := range map[string]string{
		"malformed": "{", "no tenant": `{"event_id":"e","run_id":"r"}`, "no run": `{"event_id":"e","tenant_id":"t"}`,
		"no event": `{"run_id":"r","tenant_id":"t"}`,
	} {
		if _, err := PlatformJobInput([]byte(p), PlatformJobFailedSubject, "vi-VN"); err == nil {
			t.Fatalf("%s: want error", name)
		}
	}
}
