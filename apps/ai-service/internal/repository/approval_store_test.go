package repository

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// TestValidateApprovalRunContext locks the fail-loud guard added because the
// Code Mode proposal path used to omit ExternalRun and surfaced the opaque
// ErrApprovalRunNotFound from the SQL lookup instead of a caller bug.
func TestValidateApprovalRunContext(t *testing.T) {
	full := RunContext{TenantID: "tenant-1", ActorUserID: "user-1", ExternalThread: "thread-1", ExternalRun: "run-1"}
	if err := validateApprovalRunContext(full); err != nil {
		t.Fatalf("complete run context must validate: %v", err)
	}

	cases := []struct {
		name string
		run  RunContext
		want string
	}{
		{"missing external run", RunContext{TenantID: "tenant-1", ActorUserID: "user-1"}, "external_run_id"},
		{"blank external run", RunContext{TenantID: "tenant-1", ActorUserID: "user-1", ExternalRun: "  "}, "external_run_id"},
		{"missing tenant", RunContext{ActorUserID: "user-1", ExternalRun: "run-1"}, "tenant_id"},
		{"missing actor", RunContext{TenantID: "tenant-1", ExternalRun: "run-1"}, "actor_user_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateApprovalRunContext(tc.run)
			if !errors.Is(err, ErrApprovalRunContextMissing) {
				t.Fatalf("err = %v, want ErrApprovalRunContextMissing", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want mention of %s", err, tc.want)
			}
		})
	}
}

// TestNormalizeApprovalArgumentsNeverTruncates locks the fail-closed rule for
// the executable payload: it is validated, not cut, so the action executed is
// exactly the action approved.
func TestNormalizeApprovalArgumentsNeverTruncates(t *testing.T) {
	valid := `{"customerId":"c1","note":"` + strings.Repeat("x", 20*1024) + `"}`
	got, err := normalizeApprovalArguments(valid)
	if err != nil || got != valid {
		t.Fatalf("valid payload was altered: err=%v len=%d", err, len(got))
	}

	got, err = normalizeApprovalArguments("  ")
	if err != nil || got != "{}" {
		t.Fatalf("empty payload = %q/%v, want {}", got, err)
	}

	if _, err := normalizeApprovalArguments(`{"customerId":`); !errors.Is(err, ErrApprovalArgumentsInvalid) {
		t.Fatalf("invalid JSON err = %v, want ErrApprovalArgumentsInvalid", err)
	}

	oversize := `{"blob":"` + strings.Repeat("x", maxApprovalArgumentsBytes) + `"}`
	if _, err := normalizeApprovalArguments(oversize); !errors.Is(err, ErrApprovalArgumentsInvalid) {
		t.Fatalf("oversize err = %v, want ErrApprovalArgumentsInvalid", err)
	}
}

// TestResolveExecutionArguments pins the resume-time payload resolution:
// encrypted originals round-trip, legacy rows fall back to the redacted copy
// only when it is still valid JSON, and a truncated legacy payload fails
// closed instead of executing a different action.
func TestResolveExecutionArguments(t *testing.T) {
	store := &SQLRunStore{}
	store.SetEncryptionSecret("test-encryption-secret")
	original := `{"authorization":"Bearer live-secret","customerId":"c1"}`

	encrypted, err := store.encryptSecret(original)
	if err != nil {
		t.Fatalf("encryptSecret: %v", err)
	}
	if encrypted == original || !strings.HasPrefix(encrypted, "enc:v1:") {
		t.Fatalf("payload was not encrypted at rest: %q", encrypted)
	}
	resolved, err := store.resolveExecutionArguments(
		sql.NullString{String: encrypted, Valid: true},
		sql.NullString{String: `{"authorization":"[REDACTED]"}`, Valid: true},
	)
	if err != nil || resolved != original {
		t.Fatalf("resolved = %q/%v, want original", resolved, err)
	}

	resolved, err = store.resolveExecutionArguments(
		sql.NullString{},
		sql.NullString{String: `{"customerId":"c1"}`, Valid: true},
	)
	if err != nil || resolved != `{"customerId":"c1"}` {
		t.Fatalf("legacy fallback = %q/%v", resolved, err)
	}

	if _, err := store.resolveExecutionArguments(
		sql.NullString{},
		sql.NullString{String: `{"customerId":"c1"`, Valid: true},
	); !errors.Is(err, ErrApprovalArgumentsInvalid) {
		t.Fatalf("truncated legacy payload err = %v, want ErrApprovalArgumentsInvalid", err)
	}

	noSecret := &SQLRunStore{}
	if _, err := noSecret.resolveExecutionArguments(
		sql.NullString{String: encrypted, Valid: true},
		sql.NullString{},
	); err == nil || errors.Is(err, ErrApprovalArgumentsInvalid) {
		// Missing secret is an operator/configuration failure, not a stale
		// payload: it must surface as a hard error.
		t.Fatalf("undecryptable payload err = %v, want encryption failure", err)
	}
}
