import { readdir, readFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";

const contractsURL = new URL("../contracts/ai-internal/", import.meta.url);
const policyURL = new URL("../apps/auth-gateway/configs/policy.yaml", import.meta.url);

const files = (await readdir(contractsURL)).filter((name) => name.endsWith(".json")).sort();
if (files.length === 0) throw new Error("no internal AI surface documents found in contracts/ai-internal/");

// Collect permission IDs from policy.yaml routes (indentation-agnostic).
const policy = await readFile(policyURL, "utf8");
const policyPermissions = new Set();
for (const match of policy.matchAll(/^\s+-\s+([a-z][a-z0-9.]*[a-z0-9])\s*$/gm)) {
  const id = match[1];
  if (id.includes(".")) policyPermissions.add(id);
}
if (policyPermissions.size === 0) throw new Error("no permission IDs parsed from policy.yaml");

const VALID_KINDS = new Set(["read", "confirm"]);
const VALID_RISKS = new Set(["low", "medium", "high"]);
const VALID_METHODS = new Set(["get", "post", "put", "patch", "delete"]);
const VALID_SERVICES = new Set([
  "iam-service", "crm-service", "finance-service", "hrm-service",
  "workflow-service", "notification-service", "mdm-service", "platform-service",
]);

let totalTools = 0;
const seenPaths = new Set();
const declaredServices = new Set();
const issues = [];

for (const file of files) {
  const raw = await readFile(new URL(file, contractsURL), "utf8");
  const doc = JSON.parse(raw);
  if (doc.openapi !== "3.1.0") throw new Error(`${file}: OpenAPI 3.1.0 is required`);
  if (!doc.paths || Object.keys(doc.paths).length === 0) throw new Error(`${file}: no paths`);

  for (const [route, item] of Object.entries(doc.paths)) {
    if (!route.startsWith("/internal/ai/")) {
      issues.push(`${file}: route ${route} is outside /internal/ai/* — internal AI contracts only`);
    }
    for (const [method, operation] of Object.entries(item)) {
      if (!VALID_METHODS.has(method)) continue;
      if (!operation.operationId) issues.push(`${file}: ${method.toUpperCase()} ${route} has no operationId`);
      const tool = operation["x-ai-tool"];
      if (!tool) continue;
      totalTools++;

      const at = `${file}:${method.toUpperCase()} ${route}`;
      if (!tool.sdkPath) issues.push(`${at}: x-ai-tool.sdkPath is required`);
      else if (!tool.sdkPath.startsWith("arda.")) issues.push(`${at}: sdkPath must start with arda. got ${tool.sdkPath}`);
      if (!tool.domain) issues.push(`${at}: x-ai-tool.domain is required`);
      if (tool.sdkPath && tool.domain && !tool.sdkPath.startsWith(`arda.${tool.domain}.`)) {
        issues.push(`${at}: sdkPath ${tool.sdkPath} must match arda.<domain>.<method> with domain=${tool.domain}`);
      }
      if (!tool.service) issues.push(`${at}: x-ai-tool.service is required`);
      else if (!VALID_SERVICES.has(tool.service)) issues.push(`${at}: unknown service ${tool.service}`);
      if (tool.service) declaredServices.add(tool.service.replace("-service", "").toUpperCase());
      const kind = tool.kind ?? (method === "get" ? "read" : "confirm");
      if (!VALID_KINDS.has(kind)) issues.push(`${at}: invalid kind ${kind}`);
      if (kind === "read" && method !== "get") {
        issues.push(`${at}: kind=read is only valid on GET operations`);
      }
      const risk = tool.risk ?? "medium";
      if (!VALID_RISKS.has(risk)) issues.push(`${at}: invalid risk ${risk}`);
      if (!Array.isArray(tool.requiredPerms) || tool.requiredPerms.length === 0) {
        issues.push(`${at}: requiredPerms must list at least one permission ID`);
      } else {
        for (const perm of tool.requiredPerms) {
          if (!policyPermissions.has(perm)) {
            issues.push(`${at}: permission ${perm} does not exist in auth-gateway policy.yaml — nobody can hold it`);
          }
        }
      }
      if (!Array.isArray(tool.keywords) || tool.keywords.length === 0) {
        issues.push(`${at}: keywords must list at least one search term`);
      }
      if (!tool.returns) issues.push(`${at}: returns (JSDoc @returns text) is required`);

      if (seenPaths.has(tool.sdkPath)) issues.push(`${at}: duplicate sdkPath ${tool.sdkPath}`);
      seenPaths.add(tool.sdkPath);

      // Every path parameter must be resolvable from args or scope.
      const pathParams = [...route.matchAll(/\{(\w+)\}/g)].map((m) => m[1]);
      const declared = new Set((operation.parameters ?? []).map((p) => p.name));
      for (const param of pathParams) {
        if (!declared.has(param)) {
          issues.push(`${at}: path parameter {${param}} is not declared in parameters`);
        }
      }
      const scopeParams = new Set(
        (operation.parameters ?? []).filter((p) => p["x-ai-scope"]).map((p) => p.name),
      );
      const argParams = new Set(
        (operation.parameters ?? []).filter((p) => p["x-ai-arg"] || (!p["x-ai-scope"] && (p.in === "query" || p.in === "path"))).map((p) => p.name),
      );
      for (const param of pathParams) {
        if (declared.has(param) && !scopeParams.has(param) && !argParams.has(param)) {
          issues.push(`${at}: path parameter {${param}} has neither x-ai-arg nor x-ai-scope binding`);
        }
      }
    }
  }
}

if (totalTools === 0) throw new Error("no x-ai-tool annotations found — catalog would be empty");
if (issues.length > 0) {
  for (const issue of issues) console.error(`ai-catalog: ${issue}`);
  throw new Error(`ai-catalog check failed with ${issues.length} issue(s)`);
}

// Regenerate check: the committed generated.go must match the contracts.
const gen = spawnSync("go", ["run", "./tools/catalog-gen", "--check"], {
  stdio: ["ignore", "pipe", "inherit"],
  cwd: new URL("..", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1"),
  encoding: "utf8",
});
if (gen.status !== 0) {
  throw new Error("ai-catalog: generated.go is stale — run `go run ./tools/catalog-gen` and commit");
}
console.log(gen.stdout.trim());

// Deployment wiring cross-check (WP6): every tool's service must have a
// <PREFIX>_SERVICE_URL env on the ai-service Deployment, else the tool
// silently never registers (the 2026-09-01 IAM_SERVICE_URL lesson). The
// arda-infra workspace is not part of this repository, so the check runs
// only when the sibling checkout is present (CI: skipped).
const infraPath = new URL("../../arda-infra/k8s/apps/ai-service.yaml", import.meta.url);
let infra = null;
try {
  infra = await readFile(infraPath, "utf8");
} catch {
  // Sibling checkout absent (CI) — wiring check skipped.
}
if (infra !== null) {
  const wired = new Set([...infra.matchAll(/- name: ([A-Z]+)_SERVICE_URL\b/g)].map((m) => m[1]));
  const missing = [...declaredServices].filter((d) => !wired.has(d));
  if (missing.length > 0) {
    throw new Error(
      `ai-catalog: services declared in contracts but not wired in arda-infra ai-service.yaml (missing ${missing.map((m) => `${m}_SERVICE_URL`).join(", ")}) — tools would silently never register`,
    );
  }
  console.log(`Deployment wiring OK: ${[...declaredServices].sort().join(", ")} wired in arda-infra`);
}

console.log(`AI catalog OK: ${files.length} documents, ${totalTools} tools, all permissions present in policy.yaml`);
