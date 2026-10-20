import test from "node:test"
import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import { inspectBpmn, inspectMigration } from "./check-approval-limits.mjs"

test("bắt seed sai đơn vị 100 lần như migration 20260909103000", async () => {
  const source = await readFile("apps/loan-service/migrations/20260909103000_lnm_approval_limits.sql", "utf8")
  const violations = inspectMigration("20260909103000_lnm_approval_limits.sql", source)
  assert.ok(violations.some((line) => line.includes("50_000_000_000") && line.includes("500 triệu")), violations.join("\n"))
})

test("chấp nhận migration sửa đơn vị", async () => {
  const name = "20261020100000_lnm_approval_limits_units.sql"
  const source = await readFile(`apps/loan-service/migrations/${name}`, "utf8")
  assert.deepEqual(inspectMigration(name, source), [])
})

test("hạn mức không chú thích đơn vị hoặc pgd > gd bị chặn", () => {
  const source = `
-- pgd_limit_minor = 500_000_000 = 500 triệu VND
INSERT INTO lnm_approval_limits (tenant_id, org_code, product_code, pgd_limit_minor, gd_limit_minor)
VALUES ('t', 'O', '', 500000000, 300000000);`
  const violations = inspectMigration("x.sql", source)
  assert.ok(violations.some((line) => line.includes("0 < pgd <= gd")))
  assert.ok(violations.some((line) => line.includes("300000000") && line.includes("chú thích")))
})

test("BPMN phải xét Hội đồng trước Giám đốc", async () => {
  const source = await readFile("apps/workflow-service/internal/bootstrap/lnm-loan-formation-v2.bpmn", "utf8")
  assert.deepEqual(inspectBpmn(source), [])
  const board = /<bpmn:sequenceFlow id="Flow_Level_Board"[\s\S]*?<\/bpmn:sequenceFlow>\n?\s*/.exec(source)[0]
  const swapped = source.replace(board, "").replace("<bpmn:sequenceFlow id=\"Flow_Level_Execute\"", `${board}<bpmn:sequenceFlow id="Flow_Level_Execute"`)
  assert.equal(inspectBpmn(swapped).length, 1)
})
