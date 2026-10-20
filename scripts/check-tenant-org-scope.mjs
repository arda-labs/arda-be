// AP-TEN-01 (BL-AP-003): mọi truy vấn trong apps/*/internal/repository/** đọc
// hoặc ghi một bảng có cột tenant_id phải có điều kiện tenant_id; truy vấn đọc
// hoặc ghi bảng có cột đơn vị (org_code) phải có điều kiện đơn vị. Ngoại lệ chỉ
// được phép qua check-tenant-org-scope-allowlist.json, mỗi dòng một lý do.
//
// Cách quét (tĩnh, không cần DB):
//   1. Đọc migrations/*.sql của từng service theo thứ tự (chỉ phần goose Up) để
//      biết bảng nào có tenant_id / org_code (CREATE, ADD/DROP/RENAME COLUMN,
//      RENAME TO, DROP TABLE, LIKE).
//   2. Cắt từng file Go thành khai báo cấp gói (func/const/var). Hàm "thực thi"
//      là hàm gọi Query*/Exec*/Prepare*; SQL của nó gồm chuỗi trong thân hàm và
//      chuỗi của các const/var/hàm dựng chuỗi cùng gói mà nó tham chiếu.
//   3. Bảng được tham chiếu qua FROM/JOIN/UPDATE (INSERT không xét). Điều kiện
//      được nhận ra qua dạng vị từ `[alias.]cột =`, `IN`, `= ANY`.
//
// Hạn chế đã biết (ghi rõ để không ai tưởng là chặn tuyệt đối): xét ở mức hàm,
// nên một hàm có hai câu SQL mà chỉ một câu có điều kiện vẫn lọt; SQL dựng
// từ gói khác hoặc nằm ngoài internal/repository không được quét.
import { readdir, readFile } from "node:fs/promises"
import { join, relative, resolve } from "node:path"
import { pathToFileURL } from "node:url"

const root = process.cwd()
const allowlistFile = "scripts/check-tenant-org-scope-allowlist.json"

export const DEFAULT_ORG_COLUMNS = ["org_code"]
// debt: nợ AP-TEN-01 phải sửa; by_design: xuyên tenant/đơn vị có chủ đích;
// review: cần người rà soát xác nhận; decision: chờ quyết định nghiệp vụ.
export const ALLOWLIST_KINDS = ["debt", "by_design", "review", "decision"]
const execPattern = /\.(?:Query|QueryContext|QueryRow|QueryRowContext|Exec|ExecContext|Prepare|PrepareContext|SendBatch|CopyFrom)\(/
const tableRefPattern = /\b(?:FROM|JOIN|UPDATE)\s+(?:ONLY\s+)?(?:"?[a-z_][a-z0-9_]*"?\.)?"?([a-z_][a-z0-9_]*)"?/gi

function stripDollarQuoted(sql) {
  return sql.replace(/\$([a-z_]*)\$[\s\S]*?\$\1\$/gi, "''")
}

function stripSqlComments(sql) {
  return sql.replace(/--[^\n]*/g, "").replace(/\/\*[\s\S]*?\*\//g, "")
}

function unqualified(name) {
  const parts = name.replaceAll('"', "").split(".")
  return parts[parts.length - 1].toLowerCase()
}

function splitTopLevel(body) {
  const parts = []
  let depth = 0
  let current = ""
  for (const ch of body) {
    if (ch === "(") depth += 1
    if (ch === ")") depth -= 1
    if (ch === "," && depth === 0) {
      parts.push(current)
      current = ""
    } else current += ch
  }
  if (current.trim()) parts.push(current)
  return parts
}

function goosUp(sql) {
  const index = sql.search(/^--\s*\+goose\s+Down/im)
  return index >= 0 ? sql.slice(0, index) : sql
}

// applyMigration cập nhật bản đồ bảng -> Set(cột cần theo dõi).
export function applyMigration(tables, sql, trackedColumns) {
  const tracked = new Set(trackedColumns)
  const statements = stripSqlComments(stripDollarQuoted(goosUp(sql))).split(";")
  for (const raw of statements) {
    const stmt = raw.trim()
    let match = stmt.match(/^CREATE\s+(?:UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([\w."]+)\s*\(([\s\S]*)\)/i)
    if (match) {
      const table = unqualified(match[1])
      const columns = new Set()
      for (const part of splitTopLevel(match[2])) {
        const def = part.trim()
        const like = def.match(/^LIKE\s+([\w."]+)/i)
        if (like) {
          for (const column of tables.get(unqualified(like[1])) ?? []) columns.add(column)
          continue
        }
        const column = def.match(/^"?([a-z_][a-z0-9_]*)"?\s+/i)
        if (column && tracked.has(column[1].toLowerCase())) columns.add(column[1].toLowerCase())
      }
      tables.set(table, columns)
      continue
    }
    match = stmt.match(/^DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([\w.",\s]+)/i)
    if (match) {
      for (const name of match[1].replace(/\b(CASCADE|RESTRICT)\b/gi, "").split(",")) tables.delete(unqualified(name.trim()))
      continue
    }
    match = stmt.match(/^ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?([\w."]+)\s+([\s\S]*)$/i)
    if (!match) continue
    const table = unqualified(match[1])
    const rest = match[2]
    const rename = rest.match(/^RENAME\s+TO\s+([\w."]+)/i)
    if (rename) {
      const columns = tables.get(table) ?? new Set()
      tables.delete(table)
      tables.set(unqualified(rename[1]), columns)
      continue
    }
    if (!tables.has(table)) tables.set(table, new Set())
    const columns = tables.get(table)
    const renameColumn = rest.match(/^RENAME\s+(?:COLUMN\s+)?"?(\w+)"?\s+TO\s+"?(\w+)"?/i)
    if (renameColumn) {
      columns.delete(renameColumn[1].toLowerCase())
      if (tracked.has(renameColumn[2].toLowerCase())) columns.add(renameColumn[2].toLowerCase())
      continue
    }
    for (const action of splitTopLevel(rest)) {
      const add = action.trim().match(/^ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?"?(\w+)"?\s+/i)
      if (add && !/^(CONSTRAINT|PRIMARY|UNIQUE|FOREIGN|CHECK)$/i.test(add[1]) && tracked.has(add[1].toLowerCase())) {
        columns.add(add[1].toLowerCase())
      }
      const drop = action.trim().match(/^DROP\s+(?:COLUMN\s+)?(?:IF\s+EXISTS\s+)?"?(\w+)"?/i)
      if (drop && !/^(CONSTRAINT|DEFAULT|NOT)$/i.test(drop[1])) columns.delete(drop[1].toLowerCase())
    }
  }
  return tables
}

// goDeclarations tách nguồn Go thành khai báo cấp gói: mỗi khai báo có tên,
// dòng bắt đầu, các chuỗi literal (đã bỏ comment) và các định danh xuất hiện.
export function goDeclarations(source) {
  const decls = []
  const lines = source.split(/\r?\n/)
  let current = null
  const flush = () => {
    if (current) decls.push(current)
    current = null
  }
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i]
    const top = line.match(/^(func|const|var|type|import)\b/)
    if (top) {
      flush()
      current = { kind: top[1], startLine: i + 1, text: [] }
    }
    if (current) current.text.push(line)
  }
  flush()

  const out = []
  for (const decl of decls) {
    const text = decl.text.join("\n")
    if (decl.kind === "func") {
      const name = text.match(/^func\s+(?:\(\s*\w*\s*\*?\s*(\w+)[^)]*\)\s*)?(\w+)/)
      if (!name) continue
      const scanned = scanGo(text)
      out.push({
        kind: "func",
        name: name[2],
        display: name[1] ? `${name[1]}.${name[2]}` : name[2],
        startLine: decl.startLine,
        literals: scanned.literals,
        idents: scanned.idents,
        executes: execPattern.test(scanned.code),
      })
    } else if (decl.kind === "const" || decl.kind === "var") {
      // Mỗi spec (một tên) giữ chuỗi riêng để không "mượn" điều kiện của spec khác.
      const specLines = decl.text
      const specs = []
      const single = specLines[0].match(/^(?:const|var)\s+(\w+)/)
      if (single && !/^(?:const|var)\s*\($/.test(specLines[0].trim())) {
        specs.push({ name: single[1], lines: specLines })
      } else {
        let spec = null
        for (const line of specLines.slice(1)) {
          const head = line.match(/^\t(\w+)\b(?:\s+[\w.*[\]]+)?\s*=/)
          if (head) {
            if (spec) specs.push(spec)
            spec = { name: head[1], lines: [line] }
          } else if (spec) spec.lines.push(line)
        }
        if (spec) specs.push(spec)
      }
      for (const spec of specs) {
        const scanned = scanGo(spec.lines.join("\n"))
        out.push({ kind: decl.kind, name: spec.name, display: spec.name, startLine: decl.startLine, literals: scanned.literals, idents: scanned.idents, executes: false })
      }
    }
  }
  return out
}

// scanGo trả về các chuỗi literal, định danh và mã đã bỏ comment/chuỗi.
export function scanGo(text) {
  const literals = []
  let code = ""
  let i = 0
  while (i < text.length) {
    const ch = text[i]
    const next = text[i + 1]
    if (ch === "/" && next === "/") {
      while (i < text.length && text[i] !== "\n") i += 1
      continue
    }
    if (ch === "/" && next === "*") {
      const end = text.indexOf("*/", i + 2)
      i = end < 0 ? text.length : end + 2
      continue
    }
    if (ch === "`") {
      const end = text.indexOf("`", i + 1)
      const stop = end < 0 ? text.length : end
      literals.push(text.slice(i + 1, stop))
      code += '""'
      i = stop + 1
      continue
    }
    if (ch === '"' || ch === "'") {
      let j = i + 1
      let value = ""
      while (j < text.length && text[j] !== ch && text[j] !== "\n") {
        if (text[j] === "\\") {
          value += text[j + 1] ?? ""
          j += 2
        } else {
          value += text[j]
          j += 1
        }
      }
      if (ch === '"') literals.push(value)
      code += '""'
      i = j + 1
      continue
    }
    code += ch
    i += 1
  }
  const idents = new Set(code.match(/\b[A-Za-z_]\w*\b/g) ?? [])
  return { literals, idents, code }
}

function predicatePattern(column) {
  return new RegExp(`(?:\\b\\w+\\.)?\\b${column}\\b\\s*(?:=|<>|!=|\\bIN\\b|\\bIS\\s+NOT\\s+DISTINCT\\b)|(?:=|\\bIN\\b)\\s*\\(?\\s*(?:\\b\\w+\\.)?\\b${column}\\b`, "i")
}

// analyzePackage trả về vi phạm của một gói Go (một thư mục) với bản đồ bảng
// của service chứa nó.
export function analyzePackage(files, tables, { orgColumns = DEFAULT_ORG_COLUMNS } = {}) {
  const decls = []
  for (const file of files) {
    if (file.path.endsWith("_test.go")) continue
    for (const decl of goDeclarations(file.source)) decls.push({ ...decl, file: file.path })
  }
  const helpers = new Map()
  for (const decl of decls) {
    if (decl.executes) continue
    const list = helpers.get(decl.name) ?? []
    list.push(decl)
    helpers.set(decl.name, list)
  }
  const resolveText = (decl) => {
    const seen = new Set([decl])
    const queue = [decl]
    const parts = []
    while (queue.length) {
      const item = queue.shift()
      parts.push(...item.literals)
      for (const ident of item.idents) {
        for (const helper of helpers.get(ident) ?? []) {
          if (seen.has(helper)) continue
          seen.add(helper)
          queue.push(helper)
        }
      }
    }
    return parts.join("\n")
  }
  const tenantPredicate = predicatePattern("tenant_id")
  const orgPredicates = orgColumns.map(predicatePattern)
  const violations = []
  for (const decl of decls) {
    if (!decl.executes) continue
    const sql = resolveText(decl)
    const referenced = new Set()
    for (const match of sql.matchAll(tableRefPattern)) {
      const table = match[1].toLowerCase()
      if (tables.has(table)) referenced.add(table)
    }
    for (const table of [...referenced].sort()) {
      const columns = tables.get(table)
      if (columns.has("tenant_id") && !tenantPredicate.test(sql)) {
        violations.push({ file: decl.file, func: decl.display, rule: "tenant", table, line: decl.startLine })
      }
      const orgColumn = orgColumns.find((column) => columns.has(column))
      if (orgColumn && !orgPredicates.some((pattern) => pattern.test(sql))) {
        violations.push({ file: decl.file, func: decl.display, rule: "org", table, line: decl.startLine })
      }
    }
  }
  return violations
}

export function violationKey(entry) {
  return `${entry.file}|${entry.func}|${entry.rule}|${entry.table}`
}

// compareAllowlist trả về lỗi: vi phạm mới, dòng allowlist thiếu lý do, trùng
// hoặc đã hết hiệu lực (bắt buộc xóa để allowlist chỉ co lại).
export function compareAllowlist(violations, allowlist) {
  const errors = []
  const allowed = new Map()
  for (const entry of allowlist) {
    const key = violationKey(entry)
    if (!entry.reason || !String(entry.reason).trim()) errors.push(`allowlist thiếu lý do: ${key}`)
    if (!["tenant", "org"].includes(entry.rule)) errors.push(`allowlist rule không hợp lệ: ${key}`)
    if (!ALLOWLIST_KINDS.includes(entry.kind)) errors.push(`allowlist kind phải là ${ALLOWLIST_KINDS.join("|")}: ${key}`)
    if (allowed.has(key)) errors.push(`allowlist trùng: ${key}`)
    allowed.set(key, entry)
  }
  const actual = new Set(violations.map(violationKey))
  for (const violation of violations) {
    if (!allowed.has(violationKey(violation))) {
      errors.push(`${violation.file}:${violation.line} ${violation.func}: truy vấn bảng ${violation.table} thiếu điều kiện ${violation.rule === "tenant" ? "tenant_id" : "đơn vị (org_code)"}`)
    }
  }
  for (const key of allowed.keys()) {
    if (!actual.has(key)) errors.push(`allowlist hết hiệu lực (đã sửa hoặc đổi tên, hãy xóa dòng): ${key}`)
  }
  return errors
}

async function listFiles(directory, predicate) {
  const out = []
  let entries
  try {
    entries = await readdir(directory, { withFileTypes: true })
  } catch (error) {
    if (error.code === "ENOENT") return out
    throw error
  }
  for (const entry of entries) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) out.push(...(await listFiles(path, predicate)))
    else if (entry.isFile() && predicate(entry.name)) out.push(path)
  }
  return out
}

export async function scanRepository(repoRoot, config = {}) {
  const orgColumns = config.orgColumns ?? DEFAULT_ORG_COLUMNS
  const violations = []
  let packages = 0
  for (const app of await readdir(join(repoRoot, "apps"), { withFileTypes: true })) {
    if (!app.isDirectory()) continue
    const appRoot = join(repoRoot, "apps", app.name)
    const goFiles = await listFiles(join(appRoot, "internal", "repository"), (name) => name.endsWith(".go") && !name.endsWith("_test.go"))
    if (goFiles.length === 0) continue
    const tables = new Map()
    const migrations = (await listFiles(join(appRoot, "migrations"), (name) => name.endsWith(".sql"))).sort()
    for (const file of migrations) applyMigration(tables, await readFile(file, "utf8"), ["tenant_id", ...orgColumns])
    const byDir = new Map()
    for (const file of goFiles) {
      const dir = resolve(file, "..")
      const list = byDir.get(dir) ?? []
      list.push({ path: relative(repoRoot, file).replaceAll("\\", "/"), source: await readFile(file, "utf8") })
      byDir.set(dir, list)
    }
    for (const files of byDir.values()) {
      packages += 1
      violations.push(...analyzePackage(files, tables, { orgColumns }))
    }
  }
  return { violations, packages }
}

async function main() {
  const config = JSON.parse(await readFile(resolve(root, allowlistFile), "utf8"))
  if (!Array.isArray(config.allowlist)) throw new Error(`${allowlistFile}: thiếu mảng allowlist`)
  const { violations, packages } = await scanRepository(root, config)
  const errors = compareAllowlist(violations, config.allowlist)
  if (errors.length) {
    console.error(`Tenant/org scope invariant failed (${errors.length}):`)
    for (const error of errors) console.error(`  ${error}`)
    console.error(`Sửa truy vấn để có điều kiện tenant_id/org, hoặc thêm dòng vào ${allowlistFile} kèm lý do.`)
    process.exit(1)
  }
  const tenant = violations.filter((v) => v.rule === "tenant").length
  console.log(
    `Tenant/org scope invariant OK: scanned ${packages} repository package(s); ${violations.length} allowlisted exception(s) (${tenant} tenant, ${violations.length - tenant} org)`,
  )
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  await main()
}
