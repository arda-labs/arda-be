import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";

const root = new URL("../", import.meta.url);
const issues = [];
const atLeast = (version, minimum) => {
  const left = version.split(".").map(Number);
  const right = minimum.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    if ((left[i] ?? 0) !== (right[i] ?? 0)) return (left[i] ?? 0) > (right[i] ?? 0);
  }
  return true;
};
async function inspect(path) {
  const source = await readFile(new URL(path, root), "utf8");
  const goVersion = source.match(/^go (\d+\.\d+(?:\.\d+)?)$/m)?.[1];
  if (!goVersion || !atLeast(goVersion, "1.27.2")) issues.push(`${path}: Go must be >=1.27.2`);
  for (const match of source.matchAll(/golang\.org\/x\/net v(\d+\.\d+\.\d+)/g)) {
    if (!atLeast(match[1], "0.60.0")) issues.push(`${path}: x/net must be >=v0.60.0`);
  }
}
await inspect("go.work");
for (const group of ["apps", "libs/go", "tools"]) {
  for (const entry of await readdir(new URL(`${group}/`, root), { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const path = join(group, entry.name).replaceAll("\\", "/");
    try { await readFile(new URL(`${path}/go.mod`, root)); } catch { continue; }
    await inspect(`${path}/go.mod`);
    let docker;
    try { docker = await readFile(new URL(`${path}/Dockerfile`, root), "utf8"); } catch { continue; }
    for (const match of docker.matchAll(/FROM golang:(\d+\.\d+(?:\.\d+)?)-/g)) {
      if (!atLeast(match[1], "1.27.2")) issues.push(`${path}/Dockerfile: builder must use Go >=1.27.2`);
    }
  }
}
if (issues.length) { console.error(issues.join("\n")); process.exit(1); }
console.log("Go security baseline OK: toolchain >=1.27.2 and x/net >=v0.60.0; run check-vuln.mjs for reachable findings");
