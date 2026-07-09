package handler

import "testing"

func TestRequireReviewComment(t *testing.T) {
	if err := requireReviewComment("REQUEST_CHANGES", ""); err == nil {
		t.Fatal("expected error for empty REQUEST_CHANGES comment")
	}
	if err := requireReviewComment("REJECT", "  "); err == nil {
		t.Fatal("expected error for blank REJECT comment")
	}
	if err := requireReviewComment("REQUEST_CHANGES", "Thiếu CCCD"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := requireReviewComment("APPROVE", ""); err != nil {
		t.Fatalf("APPROVE should allow empty comment: %v", err)
	}
}

func TestCheckerNotificationKeys(t *testing.T) {
	tpl, kind, title, body := checkerNotificationKeys("REQUEST_CHANGES")
	if tpl != "crm.customer.request_changes" || kind != "warning" || title == "" || body == "" {
		t.Fatalf("unexpected keys: %s %s %s %s", tpl, kind, title, body)
	}
	if tpl, _, _, _ = checkerNotificationKeys("UNKNOWN"); tpl != "" {
		t.Fatalf("expected empty for unknown decision, got %s", tpl)
	}
}
