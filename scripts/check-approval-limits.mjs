// AP-APR-01: hạn mức phê duyệt khoản vay (lnm_approval_limits) phải đúng đơn
// vị và hồ sơ thiếu cấu hình phải lên cấp cao nhất.
//
// 1. Đơn vị seed: lnm_approval_limits lưu VND theo số mũ 0 (amount_minor =
//    đồng, arda-money "VND": 0). Mọi migration ghi hạn mức phải có chú thích
//    dạng `pgd_limit_minor = 500_000_000 = 500 triệu VND`; con số và cách đọc
//    phải khớp nhau, và mọi giá trị hạn mức trong INSERT/UPDATE ... SET phải
//    là một giá trị đã chú thích trong cùng file. Từng bộ INSERT phải có
//    0 < pgd <= gd.
// 2. BPMN: ResolveApprovalLimits trả pgdLimit = gdLimit = 0 khi thiếu cấu
//    hình, dựa vào việc GW_ApprovalLevel xét nhánh Hội đồng (amount > gdLimit)
//    trước nhánh Giám đốc (amount > pgdLimit). Script giữ thứ tự đó.
import { readdir, readFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { pathToFileURL } from "node:url"

const migrationsDir = "apps/loan-service/migrations"
const bpmnPath = "apps/workflow-service/internal/bootstrap/lnm-loan-formation-v2.bpmn"

// Migration cũ đã chạy, không được sửa: seed sai đơn vị 100 lần, được migration
// thay thế chỉnh lại. Script kiểm tra file thay thế có tồn tại và có UPDATE.
export const superseded = new Map([
  ["20260909103000_lnm_approval_limits.sql", "20261020100000_lnm_approval_limits_units.sql"],
])

const scale = { "triệu": 1_000_000n, "tỷ": 1_000_000_000n }
const annotationPattern = /\b(pgd|gd)_limit_minor\s*=\s*([\d_]+)\s*=\s*([\d]+(?:[.,]\d+)?)\s*(triệu|tỷ)\s+VND\b/g

function toBigInt(text) {
  return BigInt(text.replaceAll("_", ""))
}

function humanToMinor(amount, unit) {
  const [whole, frac = ""] = amount.replace(",", ".").split(".")
  const factor = scale[unit]
  const fracScale = 10n ** BigInt(frac.length)
  return (BigInt(whole) * fracScale + BigInt(frac || "0")) * factor / fracScale
}

function stripComments(source) {
  return source.replace(/--[^\n]*/g, "")
}

// inspectMigration trả về danh sách vi phạm cho một migration có đụng tới
// lnm_approval_limits.
export function inspectMigration(name, source) {
  const violations = []
  const annotated = new Set()
  for (const match of source.matchAll(annotationPattern)) {
    const [, tier, digits, amount, unit] = match
    const declared = toBigInt(digits)
    const expected = humanToMinor(amount, unit)
    if (declared !== expected) {
      violations.push(`${name}: ${tier}_limit_minor = ${digits} nhưng chú thích ghi ${amount} ${unit} VND (= ${expected} đồng, VND số mũ 0)`)
    }
    annotated.add(declared)
  }

  const sql = stripComments(source)
  const writes = []
  for (const match of sql.matchAll(/INSERT\s+INTO\s+lnm_approval_limits\s*\(([^)]*)\)\s*VALUES\s*([\s\S]*?)(?:ON\s+CONFLICT|;)/gi)) {
    const columns = match[1].split(",").map((column) => column.trim())
    const pgdIndex = columns.indexOf("pgd_limit_minor")
    const gdIndex = columns.indexOf("gd_limit_minor")
    for (const tuple of match[2].matchAll(/\(([^()]*)\)/g)) {
      const values = tuple[1].split(",").map((value) => value.trim())
      const pgd = /^-?\d+$/.test(values[pgdIndex] ?? "") ? BigInt(values[pgdIndex]) : null
      const gd = /^-?\d+$/.test(values[gdIndex] ?? "") ? BigInt(values[gdIndex]) : null
      if (pgd === null || gd === null) {
        violations.push(`${name}: INSERT lnm_approval_limits phải ghi hạn mức bằng số nguyên trực tiếp để đối chiếu đơn vị`)
        continue
      }
      if (!(pgd > 0n && gd >= pgd)) violations.push(`${name}: bộ hạn mức (${pgd}, ${gd}) vi phạm 0 < pgd <= gd`)
      writes.push(pgd, gd)
    }
  }
  for (const match of sql.matchAll(/UPDATE\s+lnm_approval_limits\s+SET\s+([\s\S]*?)(?:\bWHERE\b|;)/gi)) {
    for (const assignment of match[1].matchAll(/\b(?:pgd|gd)_limit_minor\s*=\s*([^,\s]+)/gi)) {
      if (!/^\d+$/.test(assignment[1])) {
        violations.push(`${name}: UPDATE lnm_approval_limits phải ghi hạn mức bằng số nguyên trực tiếp để đối chiếu đơn vị`)
        continue
      }
      writes.push(BigInt(assignment[1]))
    }
  }
  for (const value of writes) {
    if (!annotated.has(value)) {
      violations.push(`${name}: hạn mức ${value} không có chú thích đơn vị (\`pgd_limit_minor = ... = N triệu|tỷ VND\`) trong cùng file`)
    }
  }
  return violations
}

// inspectBpmn kiểm tra GW_ApprovalLevel xét nhánh Hội đồng trước nhánh Giám đốc.
export function inspectBpmn(source) {
  const flows = [...source.matchAll(/<bpmn:sequenceFlow\b[^>]*sourceRef="GW_ApprovalLevel"[^>]*>([\s\S]*?)<\/bpmn:sequenceFlow>/g)]
    .map((match) => match[1])
  const board = flows.findIndex((body) => /=\s*amount\s*&gt;\s*gdLimit\b/.test(body))
  const gd = flows.findIndex((body) => /=\s*amount\s*&gt;\s*pgdLimit\b/.test(body))
  if (board < 0 || gd < 0) return [`${bpmnPath}: không tìm thấy nhánh amount > gdLimit / amount > pgdLimit của GW_ApprovalLevel`]
  if (board > gd) return [`${bpmnPath}: GW_ApprovalLevel phải xét amount > gdLimit (Hội đồng) trước amount > pgdLimit; thiếu hạn mức (0/0) sẽ không lên cấp cao nhất`]
  return []
}

async function main() {
  const violations = []
  const names = (await readdir(migrationsDir)).filter((name) => name.endsWith(".sql")).sort()
  let touched = 0
  for (const name of names) {
    const source = await readFile(join(migrationsDir, name), "utf8")
    if (!/lnm_approval_limits/.test(source)) continue
    touched++
    if (superseded.has(name)) {
      const replacement = superseded.get(name)
      if (!names.includes(replacement)) {
        violations.push(`${name}: migration thay thế ${replacement} không tồn tại`)
      } else {
        const replacementSource = await readFile(join(migrationsDir, replacement), "utf8")
        if (!/UPDATE\s+lnm_approval_limits\b/i.test(stripComments(replacementSource))) {
          violations.push(`${replacement}: phải UPDATE lnm_approval_limits để sửa seed của ${name}`)
        }
      }
      continue
    }
    violations.push(...inspectMigration(name, source))
  }
  if (touched === 0) violations.push(`${migrationsDir}: không thấy migration nào của lnm_approval_limits`)
  violations.push(...inspectBpmn(await readFile(bpmnPath, "utf8")))

  if (violations.length) {
    console.error(violations.join("\n"))
    process.exitCode = 1
    return
  }
  console.log(`Approval limit invariant OK: ${touched} migration(s) checked for VND units, Board-first escalation intact`)
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  await main()
}
