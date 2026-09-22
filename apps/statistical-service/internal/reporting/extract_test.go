package reporting

import (
	"context"
	"testing"
)

// TestExtractDailyValidatesInput locks the input contract: tenant and a
// YYYY-MM-DD business date are required before any source is contacted.
func TestExtractDailyValidatesInput(t *testing.T) {
	svc := NewService(nil, "secret", "", "", "", "", nil)

	if _, err := svc.ExtractDaily(context.Background(), "", "2026-09-22"); err == nil {
		t.Fatal("missing tenant must fail")
	}
	if _, err := svc.ExtractDaily(context.Background(), "tenant-1", "22/09/2026"); err == nil {
		t.Fatal("non-ISO business date must fail")
	}
}

// TestExtractDailySkipsUnconfiguredSources proves an environment with no
// reporting source wired reports the skip instead of touching the database.
func TestExtractDailySkipsUnconfiguredSources(t *testing.T) {
	svc := NewService(nil, "secret", "", "", "", "", nil)

	result, err := svc.ExtractDaily(context.Background(), "tenant-1", "2026-09-22")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if result.BusinessDate != "2026-09-22" {
		t.Errorf("business_date = %q", result.BusinessDate)
	}
	if len(result.SkippedSources) != 4 {
		t.Fatalf("skipped = %v, want all four sources", result.SkippedSources)
	}
}
