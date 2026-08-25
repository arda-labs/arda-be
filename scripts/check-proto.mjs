import { mkdtemp, readdir, readFile, rm } from "node:fs/promises"
import { join, relative, resolve } from "node:path"
import { execFile } from "node:child_process"
import { promisify } from "node:util"
import { fileURLToPath } from "node:url"

const execFileAsync = promisify(execFile)
const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const protoRoot = join(root, "proto")
const generatedRoot = join(root, "libs", "go", "arda-proto")
const tempRoot = await mkdtemp(join(process.env.TEMP ?? process.env.TMP ?? ".", "arda-proto-")).catch(async () => {
  return mkdtemp(join(root, ".proto-check-"))
})

async function walk(dir, predicate = () => true) {
  const files = []
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) files.push(...await walk(path, predicate))
    else if (entry.isFile() && predicate(path)) files.push(path)
  }
  return files.sort()
}

const protoFiles = await walk(protoRoot, (path) => path.endsWith(".proto"))
if (!protoFiles.length) throw new Error("No protobuf sources found")

let generationError
try {
  await execFileAsync("protoc", [
    "-I", protoRoot,
    `--go_out=${tempRoot}`,
    "--go_opt=module=github.com/arda-labs/arda/libs/go/arda-proto",
    `--go-grpc_out=${tempRoot}`,
    "--go-grpc_opt=module=github.com/arda-labs/arda/libs/go/arda-proto",
    ...protoFiles,
  ], { cwd: root })
} catch (error) {
  console.error(error.stderr ?? error.message)
  generationError = error
}

if (generationError) {
  await rm(tempRoot, { recursive: true, force: true })
  process.exit(1)
}

const generatedFiles = await walk(tempRoot, (path) => path.endsWith(".pb.go") || path.endsWith("_grpc.pb.go")).catch(() => [])
const normalize = (path) => path.replaceAll("\\\\", "/")
const expected = new Set(generatedFiles.map((path) => normalize(relative(tempRoot, path))))
const actual = new Set((await walk(generatedRoot, (path) => path.endsWith(".pb.go") || path.endsWith("_grpc.pb.go"))).map((path) => normalize(relative(generatedRoot, path))))
const missing = [...expected].filter((path) => !actual.has(path))
const extra = [...actual].filter((path) => !expected.has(path))
const mismatched = []
for (const path of expected) {
  if (!actual.has(path)) continue
  const [want, got] = await Promise.all([
    readFile(join(tempRoot, path), "utf8"),
    readFile(join(generatedRoot, path), "utf8"),
  ])
  if (want !== got) mismatched.push(path)
}

if (missing.length || extra.length || mismatched.length) {
  if (missing.length) console.error(`Missing generated files:\n${missing.join("\n")}`)
  if (extra.length) console.error(`Unexpected generated files:\n${extra.join("\n")}`)
  if (mismatched.length) console.error(`Generated files differ:\n${mismatched.join("\n")}`)
  await rm(tempRoot, { recursive: true, force: true })
  process.exit(1)
}

await rm(tempRoot, { recursive: true, force: true })
console.log(`Proto contract OK: ${protoFiles.length} sources and ${actual.size} generated files`)
