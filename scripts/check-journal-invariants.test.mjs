import test from "node:test"
import assert from "node:assert/strict"
import { inspectSource, validateCounts } from "./check-journal-invariants.mjs"

test("permits only allowlisted finance journal entry transitions", () => {
  const actual = new Map()
  for (const [file, source] of [
    ["apps/finance-service/internal/service/posting_service.go", "UPDATE fin_journal_entries SET status = 'POSTED'; UPDATE fin_journal_entries SET status = 'VOID';"],
    ["apps/finance-service/internal/repository/posting_repo.go", "UPDATE fin_journal_entries SET status = 'REVERSED';"],
  ]) {
    for (const [key, count] of inspectSource(source, file)) actual.set(key, (actual.get(key) ?? 0) + count)
  }
  assert.deepEqual(validateCounts(actual), [])
})

test("rejects journal mutations outside allowlisted files or counts", () => {
  const actual = inspectSource("DELETE FROM fin_journal_entries WHERE id = $1; UPDATE fin_journal_lines SET amount_minor=0;", "apps/finance-service/internal/repository/other.go")
  assert.equal(validateCounts(actual).length, 4)
})

test("does not gate test fixture cleanup", () => {
  assert.equal(inspectSource("DELETE FROM fin_journal_entries", "apps/finance-service/internal/service/example_test.go").size, 0)
})
