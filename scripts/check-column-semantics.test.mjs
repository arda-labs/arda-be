import test from "node:test"
import assert from "node:assert/strict"
import { inspectMigration } from "./check-column-semantics.mjs"

const file = "apps/loan-service/migrations/20261101000000_example.sql"
const wrap = (up, down = "SELECT 1;") => `-- +goose Up\n${up}\n-- +goose Down\n${down}\n`

test("chặn UPDATE đổi nghĩa cột tiền đã có (mẫu outstanding_amt_minor)", () => {
  const errors = inspectMigration(wrap(`
UPDATE lnm_agreements
SET outstanding_amt_minor = outstanding_amt_minor - pending_disburse_amt_minor,
    updated_at = now()
WHERE pending_disburse_amt_minor > 0;`), file)
  assert.equal(errors.length, 1)
  assert.match(errors[0], /lnm_agreements SET outstanding_amt_minor/)
})

test("chặn cả gán nhiều cột và bảng có alias, cột *_amt*", () => {
  const errors = inspectMigration(wrap(`
UPDATE dpm_contracts c SET (acr_interest_period_amt, status) = (c.acr_interest_amt, 'X') FROM t WHERE t.id = c.id;
UPDATE lnm_agreements AS a SET principal_minor = 0 WHERE a.id = 'x';`), file)
  assert.equal(errors.length, 2)
})

test("chặn UPDATE nằm trong khối DO $$ chạy một lần", () => {
  const errors = inspectMigration(wrap(`
DO $$
BEGIN
  UPDATE lnm_agreements SET outstanding_amt_minor = 0;
END $$;`), file)
  assert.equal(errors.length, 1)
})

test("cho phép backfill cột tiền mới thêm trong cùng migration", () => {
  const errors = inspectMigration(wrap(`
ALTER TABLE lnm_agreements ADD COLUMN IF NOT EXISTS drawn_amt_minor BIGINT NOT NULL DEFAULT 0;
UPDATE lnm_agreements SET drawn_amt_minor = outstanding_amt_minor - pending_disburse_amt_minor;`), file)
  assert.deepEqual(errors, [])
})

test("cho phép ghi bảng tạo mới, cột không phải tiền, upsert seed và thân hàm runtime", () => {
  const errors = inspectMigration(wrap(`
CREATE TABLE lnm_new (id TEXT PRIMARY KEY, amount_minor BIGINT);
UPDATE lnm_new SET amount_minor = 1;
UPDATE lnm_agreements SET status = 'ACTIVE', note = 'x_minor = 1; UPDATE a SET b_amt = 1';
INSERT INTO rpt_seed (code, threshold_minor) VALUES ('A', 1) ON CONFLICT (code) DO UPDATE SET threshold_minor = EXCLUDED.threshold_minor;
-- UPDATE lnm_agreements SET outstanding_amt_minor = 0;
CREATE OR REPLACE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE lnm_agreements SET outstanding_amt_minor = outstanding_amt_minor + NEW.amount_minor;
  RETURN NEW;
END $$;`), file)
  assert.deepEqual(errors, [])
})

test("chỉ xét phần Up", () => {
  const errors = inspectMigration(wrap("SELECT 1;", "UPDATE lnm_agreements SET outstanding_amt_minor = 0;"), file)
  assert.deepEqual(errors, [])
})

test("marker tường minh kèm lý do mở chặn; marker thiếu lý do thì không", () => {
  const body = "UPDATE lnm_agreements SET outstanding_amt_minor = 0;"
  assert.deepEqual(inspectMigration(wrap(`-- arda:column-semantics-change sửa số dư sai do lỗi settle REGISTER, đã duyệt\n${body}`), file), [])
  const errors = inspectMigration(wrap(`-- arda:column-semantics-change\n${body}`), file)
  assert.ok(errors.some((e) => /cần lý do/.test(e)))
  assert.ok(errors.some((e) => /outstanding_amt_minor/.test(e)))
})

test("migration cũ trong allowlist không bị chặn", () => {
  const legacy = "apps/loan-service/migrations/20261008120000_lnm_contract_reservations.sql"
  assert.deepEqual(inspectMigration(wrap("UPDATE lnm_agreements SET outstanding_amt_minor = 0;"), legacy), [])
})
