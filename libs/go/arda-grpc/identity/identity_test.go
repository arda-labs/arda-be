package identity

import (
	"strings"
	"testing"
	"time"
)

func TestIssueAndVerify(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token, err := Issue(strings.Repeat("s", 32), "workflow-service", "crm-service", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(token, strings.Repeat("s", 32), "crm-service", now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "workflow-service" || got.Audience != "crm-service" {
		t.Fatalf("claims = %+v", got)
	}
}

func TestVerifyRejectsWrongAudienceAndTampering(t *testing.T) {
	secret := strings.Repeat("s", 32)
	now := time.Unix(1_700_000_000, 0)
	token, err := Issue(secret, "workflow-service", "crm-service", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(token, secret, "iam-service", now); err == nil {
		t.Fatal("wrong audience was accepted")
	}
	parts := strings.Split(token, ".")
	parts[1] += "x"
	if _, err := Verify(strings.Join(parts, "."), secret, "crm-service", now); err == nil {
		t.Fatal("tampered token was accepted")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token, err := Issue(strings.Repeat("s", 32), "workflow-service", "crm-service", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(token, strings.Repeat("s", 32), "crm-service", now.Add(2*time.Second)); err == nil {
		t.Fatal("expired token was accepted")
	}
}
