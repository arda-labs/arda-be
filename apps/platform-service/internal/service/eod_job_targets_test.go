package service

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

// eodTestJobTargets is the JOB_TARGET_URLS map every EOD test seeds with: the
// in-cluster defaults from configs/config.yaml.
func eodTestJobTargets() map[string]string {
	return map[string]string{
		"loan-service":        "http://loan-service:8080",
		"deposit-service":     "http://deposit-service:8080",
		"finance-service":     "http://finance-service:8080",
		"statistical-service": "http://statistical-service:8080",
	}
}

func TestResolveEODStepsJoinsTargetAndPath(t *testing.T) {
	targets := eodTestJobTargets()
	targets["statistical-service"] = " http://statistical-service:8080/ "
	svc := NewEODService(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, targets)
	steps, err := svc.resolveEODSteps()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != len(eodStepCatalog) {
		t.Fatalf("resolved %d steps, want %d", len(steps), len(eodStepCatalog))
	}
	want := map[string]string{
		"LNM_ACCRUAL_DAILY":  "http://loan-service:8080/internal/jobs/accrual-daily",
		"DPM_ACCRUAL_DAILY":  "http://deposit-service:8080/internal/jobs/deposit-accrual-daily",
		"RPT_EVALUATE_RULES": "http://statistical-service:8080/internal/jobs/evaluate-rules",
	}
	for _, step := range steps {
		if endpoint, ok := want[step.Code]; ok && step.Endpoint != endpoint {
			t.Fatalf("%s endpoint = %q, want %q", step.Code, step.Endpoint, endpoint)
		}
		if step.Endpoint == "" || strings.Contains(step.Endpoint, "//internal") {
			t.Fatalf("%s endpoint = %q", step.Code, step.Endpoint)
		}
	}
}

func TestResolveEODStepsFailsClosedWhenTargetMissing(t *testing.T) {
	targets := eodTestJobTargets()
	delete(targets, "statistical-service")
	targets["finance-service"] = "   "
	svc := NewEODService(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, targets)
	steps, err := svc.resolveEODSteps()
	if !errors.Is(err, ErrEODJobTargetMissing) {
		t.Fatalf("resolveEODSteps error = %v, want ErrEODJobTargetMissing", err)
	}
	if steps != nil {
		t.Fatalf("partial steps returned: %v", steps)
	}
	for _, fragment := range []string{"JOB_TARGET_URLS", "FIN_TRIAL_BALANCE_DAILY (finance-service)", "RPT_EXTRACT_DAILY (statistical-service)", "RPT_RECONCILE_ACCOUNTING", "RPT_EVALUATE_RULES"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error %q does not mention %q", err, fragment)
		}
	}
	// nil map (JOB_TARGET_URLS unset) reports every step.
	svc = NewEODService(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil)
	if _, err := svc.resolveEODSteps(); !errors.Is(err, ErrEODJobTargetMissing) || !strings.Contains(err.Error(), "LNM_ACCRUAL_DAILY (loan-service)") {
		t.Fatalf("resolveEODSteps(nil targets) error = %v", err)
	}
}

// SeedJobs must not write a single row when a target is missing, so a tenant
// never ends up with a half-seeded COB graph.
func TestSeedJobsWritesNothingWhenTargetMissing(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	targets := eodTestJobTargets()
	delete(targets, "deposit-service")
	svc := NewEODService(db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, targets)
	const tenantID = "00000000-0000-0000-0000-000000000045"
	if err := svc.SeedJobs(t.Context(), tenantID); !errors.Is(err, ErrEODJobTargetMissing) {
		t.Fatalf("SeedJobs error = %v, want ErrEODJobTargetMissing", err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM plt_job_definitions WHERE tenant_id = $1`, tenantID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("seeded %d steps despite the missing target", count)
	}
}
