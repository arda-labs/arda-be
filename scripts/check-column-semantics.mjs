// AP-DAT-01: một cột một nghĩa. Migration không được UPDATE giá trị của cột
// tiền đã có (*_minor, *_amt*) để đổi nghĩa dữ liệu cũ tại chỗ; muốn đổi nghĩa
// thì thêm cột mới (ADD COLUMN / CREATE TABLE trong cùng migration rồi backfill)
// và di chuyển dữ liệu có mốc. Trường hợp thật sự phải sửa tại chỗ (chuẩn hóa
// dữ liệu lỗi, sửa số liệu sai) phải ghi marker tường minh trong phần Up:
//
//   -- arda:column-semantics-change <lý do, ít nhất 10 ký tự>
//
// Migration cũ đã làm việc này trước khi có check được liệt kê trong allowlist.
import { readdir, readFile } from "node:fs/promises"
import { join, relative, resolve, sep } from "node:path"
import { pathToFileURL } from "node:url"

const root = process.cwd()
const moneyColumn = /(?:_minor$|_amt)/i
const markerPattern = /--\s*arda:column-semantics-change\b[ \t]*(.*)$/gim
const minReasonLength = 10

// Migration đã có trên main trước check này. Không thêm mục mới: migration mới
// dùng marker kèm lý do để người review thấy quyết định đổi nghĩa.
export const allowlist = new Set([
  // Chuẩn hóa outstanding_amt_minor (trừ pending_disburse_amt_minor) khi đổi
  // công thức số dư rời nhau; dữ liệu trước mốc này mang nghĩa cũ.
  "apps/loan-service/migrations/20261008120000_lnm_contract_reservations.sql",
])

// Bỏ chú thích, thay chuỗi '...' bằng '' và bỏ thân hàm/thủ tục ($$ ... $$ sau
// CREATE FUNCTION/PROCEDURE, đó là logic runtime); thân khối DO $$ được giữ vì
// chạy một lần như câu lệnh của migration.
export function stripSql(source) {
  let out = ""
  let i = 0
  while (i < source.length) {
    const ch = source[i]
    const next = source[i + 1]
    if (ch === "-" && next === "-") {
      const end = source.indexOf("\n", i)
      i = end === -1 ? source.length : end
      continue
    }
    if (ch === "/" && next === "*") {
      const end = source.indexOf("*/", i + 2)
      i = end === -1 ? source.length : end + 2
      out += " "
      continue
    }
    if (ch === "'") {
      let j = i + 1
      while (j < source.length) {
        if (source[j] === "'" && source[j + 1] === "'") j += 2
        else if (source[j] === "'") break
        else j++
      }
      out += "''"
      i = j + 1
      continue
    }
    if (ch === "$") {
      const tag = /^\$[A-Za-z_]*\$/.exec(source.slice(i))
      if (tag) {
        const start = i + tag[0].length
        const end = source.indexOf(tag[0], start)
        const body = end === -1 ? source.slice(start) : source.slice(start, end)
        i = end === -1 ? source.length : end + tag[0].length
        const statementHead = out.slice(out.lastIndexOf(";") + 1)
        out += /\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:FUNCTION|PROCEDURE)\b/i.test(statementHead)
          ? " $body$ "
          : ` ${stripSql(body)} `
        continue
      }
    }
    out += ch
    i++
  }
  return out
}

function unquoteIdent(name) {
  return name.replace(/"/g, "").split(".").pop().toLowerCase()
}

// Bảng/cột được tạo mới trong cùng migration: backfill chúng không phải đổi nghĩa.
function newColumns(sql) {
  const tables = new Set()
  const columns = new Set()
  for (const m of sql.matchAll(/\bCREATE\s+(?:UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([\w."]+)/gi)) {
    tables.add(unquoteIdent(m[1]))
  }
  for (const m of sql.matchAll(/\bALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?([\w."]+)([^;]*)/gi)) {
    const table = unquoteIdent(m[1])
    for (const add of m[2].matchAll(/\bADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?("?\w+"?)/gi)) {
      const col = unquoteIdent(add[1])
      if (!["constraint", "primary", "unique", "check", "foreign", "exclude"].includes(col)) columns.add(`${table}.${col}`)
    }
  }
  return { tables, columns }
}

function splitTopLevel(text) {
  const parts = []
  let depth = 0
  let current = ""
  for (const ch of text) {
    if (ch === "(") depth++
    if (ch === ")") depth--
    if (ch === "," && depth === 0) {
      parts.push(current)
      current = ""
    } else current += ch
  }
  if (current.trim()) parts.push(current)
  return parts
}

// Trả về [{table, column}] cho mọi cột tiền bị UPDATE trong câu lệnh.
export function moneyAssignments(sql) {
  const found = []
  for (const statement of sql.split(";")) {
    // ON CONFLICT ... DO UPDATE SET là upsert seed, không phải sửa dữ liệu cũ.
    const m = /\bUPDATE\s+(?:ONLY\s+)?(?!SET\b)([\w."]+)(?:\s+(?:AS\s+)?(?!SET\b)\w+)?\s+SET\b([\s\S]*)/i.exec(statement)
    if (!m || /\bDO\s*$/i.test(statement.slice(0, m.index))) continue
    const table = unquoteIdent(m[1])
    let depth = 0
    let assignments = m[2]
    for (let i = 0; i < m[2].length; i++) {
      if (m[2][i] === "(") depth++
      else if (m[2][i] === ")") depth--
      else if (depth === 0 && /^\s(?:FROM|WHERE|RETURNING)\b/i.test(m[2].slice(i, i + 11))) {
        assignments = m[2].slice(0, i)
        break
      }
    }
    for (const assignment of splitTopLevel(assignments)) {
      const target = /^\s*(\([^)]*\)|"?\w+"?)\s*=/.exec(assignment)
      if (!target) continue
      for (const col of target[1].replace(/[()]/g, "").split(",")) {
        const column = unquoteIdent(col.trim())
        if (moneyColumn.test(column)) found.push({ table, column })
      }
    }
  }
  return found
}

export function inspectMigration(source, file) {
  if (allowlist.has(file)) return []
  const up = source.split(/^\s*--\s*\+goose\s+Down\b/m)[0]
  const markers = [...up.matchAll(markerPattern)]
  const errors = []
  for (const marker of markers) {
    if (marker[1].trim().length < minReasonLength) {
      errors.push(`${file}: marker arda:column-semantics-change cần lý do (>= ${minReasonLength} ký tự)`)
    }
  }
  if (markers.length && !errors.length) return []
  const sql = stripSql(up)
  const { tables, columns } = newColumns(sql)
  for (const { table, column } of moneyAssignments(sql)) {
    if (tables.has(table) || columns.has(`${table}.${column}`)) continue
    errors.push(`${file}: UPDATE ${table} SET ${column} sửa tại chỗ cột tiền đã có (có thể đổi nghĩa dữ liệu cũ); thêm cột mới, hoặc ghi "-- arda:column-semantics-change <lý do>"`)
  }
  return errors
}

async function walk(dir, errors) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) await walk(path, errors)
    else if (entry.isFile() && entry.name.endsWith(".sql") && path.split(sep).includes("migrations")) {
      const file = relative(root, path).split(sep).join("/")
      errors.push(...inspectMigration(await readFile(path, "utf8"), file))
    }
  }
}

async function main() {
  const errors = []
  await walk(join(root, "apps"), errors)
  for (const file of allowlist) {
    try {
      await readFile(join(root, file))
    } catch {
      errors.push(`${file}: allowlist trỏ tới migration không còn tồn tại`)
    }
  }
  if (errors.length) {
    console.error(errors.join("\n"))
    process.exitCode = 1
    return
  }
  console.log("Column semantics OK: no migration rewrites existing money columns without a new column or explicit marker")
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  await main()
}
