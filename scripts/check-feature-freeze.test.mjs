import test from "node:test"
import assert from "node:assert/strict"
import { findViolations, hasExceptionLabel } from "./check-feature-freeze.mjs"

const baseApps = new Set(["loan-service", "ai-service", "workflow-service"])

test("fixes inside existing services pass", () => {
  assert.deepEqual(
    findViolations(
      [
        "apps/loan-service/internal/domain/accrual_fix.go",
        "apps/loan-service/migrations/20261120000000_fix.sql",
        "apps/workflow-service/internal/service/fix_test.go",
        "docs/postgres.md",
        "scripts/lib/helper.mjs",
      ],
      baseApps,
    ),
    [],
  )
})

test("a new service is reported once, not per file", () => {
  const v = findViolations(["apps/fx-service/go.mod", "apps/fx-service/cmd/main.go", "apps/fx-service/x.bpmn"], baseApps)
  assert.equal(v.length, 1)
  assert.match(v[0], /^apps\/fx-service\/: new service/)
})

test("new BPMN, new check script and new ai-service files are blocked", () => {
  const v = findViolations(
    [
      "apps/workflow-service/internal/bootstrap/LNM.999.01.bpmn",
      "scripts/check-something.mjs",
      "apps/ai-service/internal/tools/new_tool.go",
    ],
    baseApps,
  )
  assert.equal(v.length, 3)
  assert.match(v[0], /new BPMN process/)
  assert.match(v[1], /new check script/)
  assert.match(v[2], /new ai-service file/)
})

test("the freeze gate does not block itself", () => {
  assert.deepEqual(findViolations(["scripts/check-feature-freeze.mjs", "scripts/check-feature-freeze.test.mjs"], baseApps), [])
})

test("the exception label is read from the pull_request event", () => {
  assert.equal(hasExceptionLabel({ pull_request: { labels: [{ name: "freeze-exception" }] } }), true)
  assert.equal(hasExceptionLabel({ pull_request: { labels: [{ name: "bug" }] } }), false)
  assert.equal(hasExceptionLabel(null), false)
})
