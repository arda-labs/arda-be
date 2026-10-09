package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

const eodTestTenant = "eod-integration-tenant"

type eodTestTenantDirectory struct{ ids []string }

func (d eodTestTenantDirectory) ListActiveTenants(context.Context) ([]string, error) {
	return d.ids, nil
}

type eodTestTransport struct{ target *url.URL }

func (t eodTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func newEODIntegrationFixture(t *testing.T, handler http.Handler) (*sql.DB, *EODService, time.Time) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	repo := repository.NewCalendarRepository(db)
	calendar := NewCalendarService(repo)
	state, err := repo.GetSystemDate(context.Background(), "HEAD_OFFICE")
	if err != nil || state == nil {
		t.Fatalf("load SYSTEM business date: state=%v err=%v", state, err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewEODService(db, slog.New(slog.NewTextHandler(io.Discard, nil)), calendar, eodTestTenantDirectory{ids: []string{eodTestTenant}})
	svc.client = &http.Client{Timeout: 5 * time.Second, Transport: eodTestTransport{target: target}}
	return db, svc, state.CurrentBusinessDate
}

func TestRunSystemExecutesSeededStepsInOrderAndAdvancesDate(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	db, svc, current := newEODIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-Tenant-Id") != eodTestTenant || r.Header.Get("X-User-Id") != "eod-job" {
			t.Errorf("unexpected internal request: method=%s tenant=%q user=%q", r.Method, r.Header.Get("X-Tenant-Id"), r.Header.Get("X-User-Id"))
		}
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))

	result, err := svc.RunSystem(context.Background(), current.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("RunSystem: %v", err)
	}
	want := []string{
		"/internal/jobs/accrual-daily",
		"/internal/jobs/deposit-accrual-daily",
		"/internal/jobs/provision-daily",
		"/internal/jobs/agreement-daily-snapshot",
		"/internal/jobs/trial-balance-daily",
		"/internal/jobs/report-extract-daily",
		"/internal/jobs/reconcile-accounting",
		"/internal/jobs/evaluate-rules",
	}
	if len(paths) != len(want) {
		t.Fatalf("called %d steps, want %d: %v", len(paths), len(want), paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("step %d = %q, want %q; calls=%v", i, paths[i], want[i], paths)
		}
	}
	if result.BusinessDateState == nil || !result.BusinessDateState.CurrentBusinessDate.After(current) {
		t.Fatalf("successful EOD did not advance SYSTEM date: before=%s result=%+v", current.Format("2006-01-02"), result.BusinessDateState)
	}
	var runStatus string
	if err := db.QueryRow(`SELECT status FROM plt_eod_runs WHERE eod_date=$1::date`, current.Format("2006-01-02")).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "SUCCEEDED" {
		t.Fatalf("EOD run status = %s, want SUCCEEDED", runStatus)
	}
	if _, err := svc.RunSystem(context.Background(), current.Format("2006-01-02")); err != nil {
		t.Fatalf("idempotent RunSystem replay: %v", err)
	}
	if len(paths) != len(want) {
		t.Fatalf("idempotent replay repeated job calls: got %v", paths)
	}
}

func TestRunSystemFailureDoesNotAdvanceAndRetryResumes(t *testing.T) {
	var mu sync.Mutex
	failLoanAccrual := true
	var calls = map[string]int{}
	db, svc, current := newEODIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		fail := r.URL.Path == "/internal/jobs/accrual-daily" && failLoanAccrual
		mu.Unlock()
		if fail {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	date := current.Format("2006-01-02")
	if _, err := svc.RunSystem(context.Background(), date); err == nil {
		t.Fatal("RunSystem succeeded after a mandatory step failed")
	}
	var before, status, eodStatus string
	if err := db.QueryRow(`SELECT business_date::text,status FROM plt_business_dates WHERE scope_type='SYSTEM' AND tenant_id IS NULL`).Scan(&before, &status); err != nil {
		t.Fatal(err)
	}
	if before != date || status != "OPEN" {
		t.Fatalf("failed run changed SYSTEM date gate: date=%s status=%s, want %s OPEN", before, status, date)
	}
	if err := db.QueryRow(`SELECT status FROM plt_eod_runs WHERE eod_date=$1::date`, date).Scan(&eodStatus); err != nil {
		t.Fatal(err)
	}
	if eodStatus != "FAILED" {
		t.Fatalf("failed run status = %s, want FAILED", eodStatus)
	}

	mu.Lock()
	failLoanAccrual = false
	mu.Unlock()
	result, err := svc.RunSystem(context.Background(), date)
	if err != nil {
		t.Fatalf("RunSystem retry: %v", err)
	}
	if result.BusinessDateState == nil || !result.BusinessDateState.CurrentBusinessDate.After(current) {
		t.Fatalf("successful retry did not advance date: %+v", result.BusinessDateState)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["/internal/jobs/accrual-daily"] != 2 {
		t.Fatalf("loan accrual calls = %d, want first attempt plus retry", calls["/internal/jobs/accrual-daily"])
	}
	if calls["/internal/jobs/deposit-accrual-daily"] != 1 {
		t.Fatalf("deposit accrual calls = %d, want once after successful retry", calls["/internal/jobs/deposit-accrual-daily"])
	}
}

func TestRunSystemRejectsConcurrentExecutor(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	_, svc, current := newEODIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			close(entered)
			<-release
		})
		w.WriteHeader(http.StatusNoContent)
	}))
	date := current.Format("2006-01-02")
	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.RunSystem(context.Background(), date)
		firstDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first executor did not reach its internal step")
	}
	if _, err := svc.RunSystem(context.Background(), date); !errors.Is(err, ErrEODRunInProgress) {
		t.Fatalf("concurrent executor error = %v, want ErrEODRunInProgress", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first executor: %v", err)
	}
}
