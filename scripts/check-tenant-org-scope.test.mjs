import test from "node:test"
import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import { fileURLToPath } from "node:url"
import { analyzePackage, applyMigration, compareAllowlist, scanRepository } from "./check-tenant-org-scope.mjs"

const tracked = ["tenant_id", "org_code"]

function tablesFrom(...migrations) {
  const tables = new Map()
  for (const sql of migrations) applyMigration(tables, sql, tracked)
  return tables
}

const baseTables = () =>
  tablesFrom(`-- +goose Up
CREATE TABLE IF NOT EXISTS lnm_contracts (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  amount NUMERIC(18, 2),
  UNIQUE (tenant_id, id)
);
CREATE TABLE plt_codes (id TEXT PRIMARY KEY, name TEXT);
ALTER TABLE lnm_contracts ADD COLUMN IF NOT EXISTS org_code VARCHAR(64);
-- +goose Down
ALTER TABLE lnm_contracts DROP COLUMN org_code;
`)

function pkg(source) {
  return [{ path: "apps/loan-service/internal/repository/repo.go", source }]
}

test("đọc cột tenant/org từ migration goose Up, bỏ qua phần Down", () => {
  const tables = baseTables()
  assert.deepEqual([...tables.get("lnm_contracts")].sort(), ["org_code", "tenant_id"])
  assert.deepEqual([...tables.get("plt_codes")], [])
})

test("theo dõi RENAME TO, RENAME COLUMN, DROP COLUMN và DROP TABLE", () => {
  const tables = tablesFrom(
    "CREATE TABLE public.a (id TEXT, tenant TEXT, org_code TEXT);",
    "ALTER TABLE public.a RENAME COLUMN tenant TO tenant_id;",
    "ALTER TABLE a DROP COLUMN IF EXISTS org_code;",
    "ALTER TABLE a RENAME TO b;",
    "CREATE TABLE c (id TEXT, tenant_id TEXT); DROP TABLE IF EXISTS c;",
  )
  assert.deepEqual([...tables.get("b")], ["tenant_id"])
  assert.equal(tables.has("a"), false)
  assert.equal(tables.has("c"), false)
})

test("chặn truy vấn theo id thiếu tenant_id và thiếu org_code", () => {
  const violations = analyzePackage(
    pkg(`package repository

func (r *LoanRepository) GetContract(ctx context.Context, id string) error {
	return r.db.QueryRowContext(ctx, \`SELECT amount FROM lnm_contracts WHERE id = $1\`, id).Scan(nil)
}
`),
    baseTables(),
  )
  assert.deepEqual(
    violations.map((v) => `${v.func}|${v.rule}|${v.table}`),
    ["LoanRepository.GetContract|tenant|lnm_contracts", "LoanRepository.GetContract|org|lnm_contracts"],
  )
})

test("chấp nhận truy vấn có điều kiện tenant và org (kể cả alias, = ANY)", () => {
  const violations = analyzePackage(
    pkg(`package repository

func (r *LoanRepository) GetContract(ctx context.Context, tenantID, id string, orgs []string) error {
	return r.db.QueryRowContext(ctx, \`SELECT c.amount FROM lnm_contracts c
		WHERE c.tenant_id = $1 AND c.id = $2 AND c.org_code = ANY($3)\`, tenantID, id, orgs).Scan(nil)
}

func (r *LoanRepository) Touch(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE plt_codes SET name = 'x' WHERE id = $1", id)
	return err
}
`),
    baseTables(),
  )
  assert.deepEqual(violations, [])
})

test("ghép SQL từ const/hàm dựng chuỗi cùng gói, không mượn điều kiện của const khác", () => {
  const source = `package repository

const (
	contractSelect = \`SELECT amount FROM lnm_contracts\`
	unrelated      = "tenant_id = $9 AND org_code = $8"
)

func contractWhere() string { return " WHERE tenant_id = $1 AND org_code = ANY($2) AND id = $3" }

func (r *LoanRepository) Scoped(ctx context.Context) error {
	return r.db.QueryRowContext(ctx, contractSelect+contractWhere()).Scan(nil)
}

func (r *LoanRepository) Unscoped(ctx context.Context) error {
	// tenant_id = $1 trong comment không được tính
	return r.db.QueryRowContext(ctx, contractSelect+" WHERE id = $1").Scan(nil)
}
`
  const violations = analyzePackage(pkg(source), baseTables())
  assert.deepEqual(
    violations.map((v) => `${v.func}|${v.rule}`),
    ["LoanRepository.Unscoped|tenant", "LoanRepository.Unscoped|org"],
  )
})

test("bỏ qua file _test.go và INSERT", () => {
  const violations = analyzePackage(
    [
      { path: "apps/x/internal/repository/repo_test.go", source: "package repository\nfunc f() { db.ExecContext(ctx, `DELETE FROM lnm_contracts`) }\n" },
      { path: "apps/x/internal/repository/repo.go", source: "package repository\nfunc g() { db.ExecContext(ctx, `INSERT INTO lnm_contracts (id) VALUES ($1)`) }\n" },
    ],
    baseTables(),
  )
  assert.deepEqual(violations, [])
})

test("allowlist: vi phạm mới fail, dòng thiếu lý do/kind fail, dòng hết hiệu lực fail", () => {
  const violation = { file: "f.go", func: "R.Get", rule: "org", table: "t", line: 3 }
  assert.equal(compareAllowlist([violation], []).length, 1)
  assert.deepEqual(compareAllowlist([violation], [{ ...violation, kind: "debt", reason: "nợ" }]), [])
  assert.equal(compareAllowlist([violation], [{ ...violation, kind: "debt", reason: " " }]).length, 1)
  assert.equal(compareAllowlist([violation], [{ ...violation, kind: "whatever", reason: "x" }]).length, 1)
  assert.equal(compareAllowlist([], [{ ...violation, kind: "debt", reason: "nợ" }]).length, 1)
})

test("mã hiện tại khớp đúng allowlist đã commit", async () => {
  const config = JSON.parse(await readFile(new URL("./check-tenant-org-scope-allowlist.json", import.meta.url), "utf8"))
  const { violations } = await scanRepository(fileURLToPath(new URL("..", import.meta.url)), config)
  assert.deepEqual(compareAllowlist(violations, config.allowlist), [])
})
