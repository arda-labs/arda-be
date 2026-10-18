package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

type fakeInterestConfigStore struct {
	rows  []domain.InterestConfigRecord
	err   error
	calls int
	// last lookup
	tenant, product, org string
}

func (f *fakeInterestConfigStore) ListInterestConfigVersions(_ context.Context, tenant, product, org string) ([]domain.InterestConfigRecord, error) {
	f.calls++
	f.tenant, f.product, f.org = tenant, product, org
	return f.rows, f.err
}

func wantInterestCode(t *testing.T, err error, code string) {
	t.Helper()
	var ie *domain.InterestError
	if !errors.As(err, &ie) || ie.Code != code {
		t.Fatalf("error = %v, want coded error %s", err, code)
	}
}

func TestInterestConfigResolverValidatesTheRequestBeforeTheDatabase(t *testing.T) {
	store := &fakeInterestConfigStore{}
	r := NewInterestConfigResolver(store)
	ctx := context.Background()
	for _, c := range []struct{ tenant, product, asOf string }{
		{"", "SP001", "2026-06-01"},
		{"t1", "", "2026-06-01"},
		{"t1", "SP001", ""},
		{"t1", "SP001", "2026-6-1"},
	} {
		_, err := r.Resolve(ctx, c.tenant, c.product, "14", c.asOf)
		wantInterestCode(t, err, domain.ErrCodeProductConfigRequestInvalid)
	}
	if store.calls != 0 {
		t.Fatalf("store was queried %d times for an invalid request", store.calls)
	}
}

func TestInterestConfigResolverMissingConfigIsACodedError(t *testing.T) {
	r := NewInterestConfigResolver(&fakeInterestConfigStore{})
	_, err := r.Resolve(context.Background(), "t1", "SP001", "14", "2026-06-01")
	wantInterestCode(t, err, domain.ErrCodeProductConfigNotFound)
}

func TestInterestConfigResolverPassesTheKeyAndKeepsStoreErrors(t *testing.T) {
	boom := errors.New("connection reset")
	store := &fakeInterestConfigStore{err: boom}
	r := NewInterestConfigResolver(store)
	_, err := r.Resolve(context.Background(), "t1", "SP001", "14", "2026-06-01")
	if !errors.Is(err, boom) {
		t.Fatalf("store failure must surface as itself (not as 'not found'), got %v", err)
	}
	if store.tenant != "t1" || store.product != "SP001" || store.org != "14" {
		t.Fatalf("lookup key = %s/%s/%s", store.tenant, store.product, store.org)
	}
}

func TestInterestConfigResolverAgainstPostgres(t *testing.T) {
	const tenant = "00000000-0000-0000-0000-0000000000b1"
	const other = "00000000-0000-0000-0000-0000000000b2"
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := context.Background()
	for _, p := range [][2]string{{tenant, "SP001"}, {tenant, "SP002"}, {other, "SP001"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO lnm_products (id, tenant_id, code, name) VALUES ($1,$2,$3,$3)`,
			p[0]+p[1], p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(tenantID, product string, org any, from string, roundNo int, extra string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO lnm_product_interest_config (tenant_id, product_code, org_code, effective_from,
				interest_base_code, interest_calc_method, interest_date_method, interest_start_method,
				overdue_check_cond, round_no, round_type, principal_grace_days, overdue_rate_type, overdue_rate_value)
			VALUES ($1,$2,$3,$4,'CSTL_02','CM123.001','CM124.002','CM125.001','CM126.001',$5,'CM163.001',`+extra+`)`,
			tenantID, product, org, from, roundNo); err != nil {
			t.Fatal(err)
		}
	}
	insert(tenant, "SP001", nil, "2026-01-01", 1000, "NULL, NULL, NULL")
	insert(tenant, "SP001", nil, "2026-06-01", 100, "10, 'CM127.001', 130.5")
	insert(tenant, "SP001", "14", "2026-03-01", 500, "NULL, NULL, NULL")
	insert(tenant, "SP002", "14", "2026-01-01", 7, "NULL, NULL, NULL")
	insert(other, "SP001", nil, "2026-01-01", 1, "NULL, NULL, NULL")

	r := NewInterestConfigResolver(repository.NewLoanRepository(db))
	got, err := r.Resolve(ctx, tenant, "SP001", "14", "2026-07-01")
	if err != nil || got.RoundNo != 500 || got.OrgCode != "14" {
		t.Fatalf("org 14 on 2026-07-01 = %+v, %v; want the org version (round_no 500)", got, err)
	}
	got, err = r.Resolve(ctx, tenant, "SP001", "15", "2026-07-01")
	if err != nil || got.RoundNo != 100 || got.OrgCode != "" {
		t.Fatalf("org 15 on 2026-07-01 = %+v, %v; want the newest tenant-wide version (round_no 100)", got, err)
	}
	if days, err := got.GraceDays(); err != nil || days != 10 {
		t.Fatalf("GraceDays() = %d, %v", days, err)
	}
	if rate, err := got.OverdueRate(); err != nil || rate.String() != "130.5" {
		t.Fatalf("OverdueRate() = %s, %v", rate, err)
	}
	got, err = r.Resolve(ctx, tenant, "SP001", "15", "2026-05-31")
	if err != nil || got.RoundNo != 1000 {
		t.Fatalf("org 15 on 2026-05-31 = %+v, %v; want the first tenant-wide version", got, err)
	}
	if _, err := got.GraceDays(); err == nil {
		t.Fatal("grace days stored as NULL must stay unconfigured")
	}
	// Another product's org row and another tenant's rows never leak in.
	_, err = r.Resolve(ctx, tenant, "SP001", "14", "2025-12-31")
	wantInterestCode(t, err, domain.ErrCodeProductConfigNotFound)
	_, err = r.Resolve(ctx, tenant, "SP002", "99", "2026-07-01")
	wantInterestCode(t, err, domain.ErrCodeProductConfigNotFound)
	got, err = r.Resolve(ctx, other, "SP001", "14", "2026-07-01")
	if err != nil || got.RoundNo != 1 {
		t.Fatalf("other tenant = %+v, %v; want its own row (round_no 1)", got, err)
	}
	// No org on the agreement: tenant-wide rows only.
	got, err = r.Resolve(ctx, tenant, "SP001", "", "2026-07-01")
	if err != nil || got.RoundNo != 100 {
		t.Fatalf("no org = %+v, %v", got, err)
	}
}
