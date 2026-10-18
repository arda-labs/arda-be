package repository

import (
	"database/sql"
	"maps"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

// Schema contract of lnm_product_interest_config. The table is the only place
// the interest parameters of a loan product live, so the database must refuse
// every value the calculator would have to guess about: closed code sets, no
// defaults, one row per (tenant, product, org-or-tenant-wide, effective_from).

const picTenant = "00000000-0000-0000-0000-0000000000a1"

// picValues are the columns of one valid configuration row. Tests copy the map
// and break one column at a time.
func picValid() map[string]string {
	return map[string]string{
		"tenant_id":             "'" + picTenant + "'",
		"product_code":          "'SP001'",
		"effective_from":        "'2026-01-01'",
		"interest_base_code":    "'CSTL_02'",
		"interest_calc_method":  "'CM123.001'",
		"interest_date_method":  "'CM124.002'",
		"interest_start_method": "'CM125.001'",
		"overdue_check_cond":    "'CM126.001'",
		"round_no":              "1000",
		"round_type":            "'CM163.001'",
	}
}

func picInsert(db *sql.DB, t *testing.T, values map[string]string, drop ...string) error {
	t.Helper()
	for _, c := range drop {
		delete(values, c)
	}
	cols, vals := make([]string, 0, len(values)), make([]string, 0, len(values))
	for c, v := range values {
		cols = append(cols, c)
		vals = append(vals, v)
	}
	_, err := db.ExecContext(t.Context(), `INSERT INTO lnm_product_interest_config (`+strings.Join(cols, ",")+`) VALUES (`+strings.Join(vals, ",")+`)`)
	return err
}

func picDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	t.Cleanup(func() { db.Close() })
	for _, code := range []string{"SP001", "SP002"} {
		if _, err := db.ExecContext(t.Context(),
			`INSERT INTO lnm_products (id, tenant_id, code, name) VALUES ($1, $2, $3, $3)`, "p-"+code, picTenant, code); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestInterestConfigSchemaAcceptsValidAndSourceCodes(t *testing.T) {
	db := picDB(t)
	if err := picInsert(db, t, picValid()); err != nil {
		t.Fatalf("valid row rejected: %v", err)
	}
	var version int
	var createdAt, updatedAt sql.NullTime
	if err := db.QueryRowContext(t.Context(),
		`SELECT version, created_at, updated_at FROM lnm_product_interest_config`).Scan(&version, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if version != 1 || !createdAt.Valid || !updatedAt.Valid {
		t.Fatalf("audit columns = version %d created %v updated %v", version, createdAt, updatedAt)
	}

	// Values that exist at the source must be storable even though the
	// resolver refuses them later: no data is lost on import.
	for name, override := range map[string]map[string]string{
		"CSTL_01 (undefined year basis)": {"interest_base_code": "'CSTL_01'"},
		"CM123.002":                      {"interest_calc_method": "'CM123.002'"},
		"CM125.002":                      {"interest_start_method": "'CM125.002'"},
		"CM126.002":                      {"overdue_check_cond": "'CM126.002'"},
		"CM124.001":                      {"interest_date_method": "'CM124.001'"},
		"round_no 0":                     {"round_no": "0"},
		"round_no 7 (any non-negative)":  {"round_no": "7"},
		"CM163.002":                      {"round_type": "'CM163.002'"},
		"CM163.003":                      {"round_type": "'CM163.003'"},
	} {
		v := picValid()
		v["effective_from"] = "'2026-02-01'" // distinct from the first row
		v["org_code"] = "'" + strings.ReplaceAll(name, "'", "") + "'"
		maps.Copy(v, override)
		if err := picInsert(db, t, v); err != nil {
			t.Errorf("%s: must be storable, got %v", name, err)
		}
	}
}

func TestInterestConfigSchemaRejectsValuesOutsideClosedSets(t *testing.T) {
	db := picDB(t)
	for name, override := range map[string]map[string]string{
		"year basis CSTL_365":        {"interest_base_code": "'CSTL_365'"},
		"year basis 365":             {"interest_base_code": "'365'"},
		"year basis lowercase":       {"interest_base_code": "'cstl_02'"},
		"year basis empty":           {"interest_base_code": "''"},
		"CM123.003":                  {"interest_calc_method": "'CM123.003'"},
		"CM124.0021 (superstring)":   {"interest_date_method": "'CM124.0021'"},
		"CM124 padded":               {"interest_date_method": "' CM124.002'"},
		"CM124 empty":                {"interest_date_method": "''"},
		"CM125.003":                  {"interest_start_method": "'CM125.003'"},
		"CM126 empty":                {"overdue_check_cond": "''"},
		"CM126.003":                  {"overdue_check_cond": "'CM126.003'"},
		"round_no negative":          {"round_no": "-1"},
		"round_type ROUND (EPAS)":    {"round_type": "'ROUND'"},
		"round_type CM163.004":       {"round_type": "'CM163.004'"},
		"round_type empty":           {"round_type": "''"},
		"org_code blank":             {"org_code": "'  '"},
		"product not in the catalog": {"product_code": "'NOPE'"},
	} {
		v := picValid()
		maps.Copy(v, override)
		if err := picInsert(db, t, v); err == nil {
			t.Errorf("%s: row accepted, want a constraint violation", name)
		}
	}
}

func TestInterestConfigSchemaHasNoDefaultsForConfigColumns(t *testing.T) {
	db := picDB(t)
	for _, col := range []string{
		"tenant_id", "product_code", "effective_from", "interest_base_code", "interest_calc_method",
		"interest_date_method", "interest_start_method", "overdue_check_cond", "round_no", "round_type",
	} {
		if err := picInsert(db, t, picValid(), col); err == nil {
			t.Errorf("row without %s accepted: the column must be mandatory with no default", col)
		}
	}
	var withDefault []string
	rows, err := db.QueryContext(t.Context(), `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'lnm_product_interest_config' AND column_default IS NOT NULL
		ORDER BY column_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		withDefault = append(withDefault, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// Only technical columns may have a default; no business value does.
	if got, want := strings.Join(withDefault, ","), "created_at,id,updated_at,version"; got != want {
		t.Fatalf("columns with a default = %s, want %s", got, want)
	}
}

func TestInterestConfigSchemaOverdueAndGraceStayUnconfiguredByDefault(t *testing.T) {
	db := picDB(t)
	// Business has not confirmed grace days or the overdue rate yet: a row
	// without them must be storable and must read back as NULL, never 0.
	if err := picInsert(db, t, picValid()); err != nil {
		t.Fatal(err)
	}
	var grace sql.NullInt64
	var rateType sql.NullString
	var rateValue sql.NullString
	if err := db.QueryRowContext(t.Context(),
		`SELECT principal_grace_days, overdue_rate_type, overdue_rate_value::text FROM lnm_product_interest_config`).
		Scan(&grace, &rateType, &rateValue); err != nil {
		t.Fatal(err)
	}
	if grace.Valid || rateType.Valid || rateValue.Valid {
		t.Fatalf("unconfirmed columns = %v %v %v, want all NULL", grace, rateType, rateValue)
	}

	ok := picValid()
	ok["effective_from"] = "'2026-03-01'"
	ok["principal_grace_days"] = "10"
	ok["overdue_rate_type"] = "'CM127.001'"
	ok["overdue_rate_value"] = "130"
	if err := picInsert(db, t, ok); err != nil {
		t.Fatalf("complete overdue configuration rejected: %v", err)
	}

	for name, override := range map[string]map[string]string{
		"negative grace":                 {"principal_grace_days": "-1"},
		"rate type unknown":              {"overdue_rate_type": "'CM127.002'", "overdue_rate_value": "130"},
		"negative rate":                  {"overdue_rate_type": "'CM127.001'", "overdue_rate_value": "-1"},
		"rate type without value":        {"overdue_rate_type": "'CM127.001'"},
		"rate value without type (EPAS)": {"overdue_rate_value": "150"},
	} {
		v := picValid()
		v["effective_from"] = "'2026-04-01'"
		maps.Copy(v, override)
		if err := picInsert(db, t, v); err == nil {
			t.Errorf("%s: row accepted, want a constraint violation", name)
		}
	}
}

func TestInterestConfigSchemaVersionUniqueness(t *testing.T) {
	db := picDB(t)
	if err := picInsert(db, t, picValid()); err != nil {
		t.Fatal(err)
	}
	// Same tenant-wide version twice: org_code NULL must still collide.
	if err := picInsert(db, t, picValid()); err == nil {
		t.Error("duplicate tenant-wide (tenant, product, effective_from) accepted")
	}
	// A later version, another product, another tenant-scoped org: all fine.
	later := picValid()
	later["effective_from"] = "'2026-06-01'"
	if err := picInsert(db, t, later); err != nil {
		t.Errorf("later version rejected: %v", err)
	}
	other := picValid()
	other["product_code"] = "'SP002'"
	if err := picInsert(db, t, other); err != nil {
		t.Errorf("other product rejected: %v", err)
	}
	org := picValid()
	org["org_code"] = "'14'"
	if err := picInsert(db, t, org); err != nil {
		t.Errorf("org-specific row with the same date as the tenant-wide row rejected: %v", err)
	}
	if err := picInsert(db, t, org); err == nil {
		t.Error("duplicate org-specific version accepted")
	}
}

func TestInterestConfigSchemaSourceKeyIsUniquePerTenant(t *testing.T) {
	db := picDB(t)
	a := picValid()
	a["source_key"] = "'14:SP001:2026-01-01'"
	if err := picInsert(db, t, a); err != nil {
		t.Fatal(err)
	}
	b := picValid()
	b["product_code"] = "'SP002'"
	b["source_key"] = "'14:SP001:2026-01-01'"
	if err := picInsert(db, t, b); err == nil {
		t.Error("same source_key twice in one tenant accepted: an import re-run would duplicate rows")
	}
	// NULL source_key (manually entered rows) never collide.
	c, d := picValid(), picValid()
	c["effective_from"], d["effective_from"] = "'2026-07-01'", "'2026-08-01'"
	if err := picInsert(db, t, c); err != nil {
		t.Error(err)
	}
	if err := picInsert(db, t, d); err != nil {
		t.Error(err)
	}
}
