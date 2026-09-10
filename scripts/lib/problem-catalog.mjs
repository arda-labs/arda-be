// Shared helpers for the problem-details catalog (docs/problems/*.md) consumed
// by check-problem-catalog.mjs (CI gate) and build-problem-docs.mjs (static
// site). Front-matter uses a deliberately tiny YAML subset (flat keys, | and >
// block scalars, simple lists) so the toolchain stays dependency-free.

import { readFile } from "node:fs/promises";

export const DOCS_BASE = "https://docs.arda.io.vn/problems/";
export const CATALOG_DIR = new URL("../../docs/problems/", import.meta.url);

// Dotted machine codes: validation.required, ai.model_unavailable, ...
export const DOTTED_CODE_RE = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$/;
// auth-gateway machine slugs (isMachineErrorCode): insufficient_permissions, ...
export const SNAKE_SLUG_RE = /^[a-z][a-z0-9_]*_[a-z0-9_]+$/;

export const REQUIRED_FIELDS = ["code", "status", "title", "summary"];

const STATUS_TEXT = new Map([
  [400, "Bad Request"], [401, "Unauthorized"], [403, "Forbidden"],
  [404, "Not Found"], [405, "Method Not Allowed"], [409, "Conflict"],
  [413, "Payload Too Large"], [422, "Unprocessable Entity"],
  [429, "Too Many Requests"], [500, "Internal Server Error"],
  [502, "Bad Gateway"], [503, "Service Unavailable"], [504, "Gateway Timeout"],
]);

export function statusText(status) {
  return STATUS_TEXT.get(status) ?? `HTTP ${status}`;
}

// Titles for canonical arda-errors codes; others derive from the code itself.
const TITLES = new Map([
  ["common.error.unknown", "Unknown error"],
  ["common.error.internal", "Internal server error"],
  ["common.error.bad_gateway", "Upstream service error"],
  ["auth.error.unauthorized", "Authentication required"],
  ["auth.error.forbidden", "Permission denied"],
  ["auth.error.user_context_required", "Authenticated user context required"],
  ["common.error.not_found", "Resource not found"],
  ["common.error.conflict", "Resource conflict"],
  ["common.error.method_not_allowed", "Method not allowed"],
  ["validation.invalid_json", "Request body is not valid JSON"],
  ["validation.invalid_input", "Request is invalid"],
  ["validation.required", "Required field is missing"],
  ["tenant.error.scope_required", "Tenant scope required"],
  ["tenant.error.migration_required", "Tenant migration required"],
  ["iam.user.not_found", "User not found"],
  ["iam.role.not_found", "Role not found"],
  ["iam.permission.not_found", "Permission not found"],
  ["iam.superadmin.last_active", "Cannot remove the last active superadmin"],
  ["iam.superadmin.system_user_protected", "System superadmin user is protected"],
  ["iam.superadmin.role_protected", "System superadmin role is protected"],
  ["iam.superadmin.permission_protected", "System superadmin permission is protected"],
  ["iam.session.limit_reached", "Maximum concurrent sessions reached"],
]);

export function deriveTitle(code) {
  if (TITLES.has(code)) return TITLES.get(code);
  const leaf = code.split(".").at(-1).replaceAll("_", " ");
  return leaf.charAt(0).toUpperCase() + leaf.slice(1);
}

const STATUS_DEFAULTS = new Map([
  [400, {
    summary: "The server rejected the request because its payload or parameters failed validation.",
    client: "Do not retry unchanged. Fix the request (see `errors[]` for the failing fields) and send it again.",
    operator: "Trace `request_id`, compare the payload against the endpoint's OpenAPI schema, and check whether the caller sent a field with the wrong type or format.",
  }],
  [401, {
    summary: "The request carries no usable authenticated session or bearer token.",
    client: "Start or refresh the login flow, then repeat the request with a fresh session. Do not try to repair it by adding identity headers.",
    operator: "Trace `request_id`, inspect cookie/CORS/origin handling, and confirm the auth-gateway can resolve the IAM user context.",
  }],
  [403, {
    summary: "The authenticated actor is not allowed to perform this action.",
    client: "Do not retry unchanged. Show access denied and guide the user to request the missing permission.",
    operator: "Trace `request_id` and check the actor's roles/permissions and the auth-gateway policy route matched for the request.",
  }],
  [404, {
    summary: "The requested resource does not exist, or the actor is not allowed to see it.",
    client: "Do not retry unchanged. Verify the identifier and refresh any stale list views.",
    operator: "Trace `request_id`, confirm the resource exists in the owning service's database, and check tenant scoping of the lookup.",
  }],
  [405, {
    summary: "The HTTP method is not supported by the requested route.",
    client: "Do not retry. Fix the client to call the documented method for this route.",
    operator: "Check whether a client or proxy is calling an outdated path or method, and compare with the published OpenAPI document.",
  }],
  [409, {
    summary: "The request conflicts with the current state of the resource.",
    client: "Reload the resource, re-apply the change on top of the fresh state, and retry once.",
    operator: "Trace `request_id`, inspect the current resource state, and check for concurrent writers or a duplicate business key.",
  }],
  [429, {
    summary: "The client sent too many requests in the current time window.",
    client: "Retry later with exponential backoff and honour the `Retry-After` response header when present.",
    operator: "Check rate-limit counters and whether a misbehaving client or retry loop is generating the traffic spike.",
  }],
  [500, {
    summary: "An unexpected error occurred while the server processed the request.",
    client: "Retry a limited number of times with backoff. If it persists, report the `request_id` to the operations team.",
    operator: "Correlate `request_id` and `trace_id` in the service logs for the failing handler, and check recent deployments and migrations.",
  }],
  [502, {
    summary: "An upstream dependency answered with an error or timed out.",
    client: "Retry later with backoff. The failure is transient and not caused by the request payload.",
    operator: "Check the health of the upstream dependency named in the logs and the `trace_id` for the failing hop.",
  }],
]);

export function defaultExample(code, status) {
  const problem = {
    type: DOCS_BASE + code,
    title: statusText(status),
    status,
    code,
    message: "<human-readable message>",
    request_id: "<request_id>",
    trace_id: "<trace_id>",
  };
  const body = JSON.stringify(problem, null, 2);
  return `HTTP/1.1 ${status} ${statusText(status)}\nContent-Type: application/problem+json\n\n${body}`;
}

export function renderPageMarkdown(page) {
  const def = STATUS_DEFAULTS.get(page.status) ?? STATUS_DEFAULTS.get(500);
  const client = page.client_action ?? def.client;
  const operator = page.operator_action ?? def.operator;
  const example = page.example ?? defaultExample(page.code, page.status);
  return { client, operator, example };
}

// --- front-matter parsing (tiny YAML subset) -------------------------------

function stripQuotes(value) {
  if (value.length >= 2) {
    const first = value[0];
    if ((first === '"' || first === "'") && value.at(-1) === first) {
      return value.slice(1, -1);
    }
  }
  return value;
}

// Returns { data, body } or throws with a descriptive message.
export function parseFrontMatter(raw, filename) {
  if (!raw.startsWith("---\n") && !raw.startsWith("---\r\n")) {
    throw new Error(`${filename}: missing front-matter block (must start with ---)`);
  }
  const lines = raw.split(/\r?\n/);
  let cursor = 1;
  while (cursor < lines.length && lines[cursor] !== "---") {
    cursor++;
  }
  if (cursor >= lines.length) {
    throw new Error(`${filename}: unterminated front-matter block (no closing ---)`);
  }
  const data = {};
  let key = null;
  let blockMode = null; // "|" literal block, ">" folded block
  let blockIndent = 0;
  let blockLines = [];
  let listItems = null;

  const flush = () => {
    if (key === null) return;
    if (blockMode === "|") {
      data[key] = blockLines.join("\n").replace(/\s+$/, "");
    } else if (blockMode === ">") {
      data[key] = blockLines.join(" ").replace(/\s+/g, " ").trim();
    } else if (listItems !== null) {
      data[key] = listItems;
    } else if (blockLines.length > 0) {
      data[key] = blockLines.join("\n").replace(/\s+$/, "");
    }
    key = null;
    blockMode = null;
    blockIndent = 0;
    blockLines = [];
    listItems = null;
  };

  for (let i = 1; i < cursor; i++) {
    const line = lines[i];
    if (line.trim() === "") {
      // Blank line: paragraph break inside a block scalar, ignored elsewhere.
      if (key !== null && blockMode !== null) blockLines.push("");
      continue;
    }
    const indent = line.length - line.trimStart().length;
    if (key !== null && blockMode !== null && indent >= blockIndent && blockIndent > 0) {
      blockLines.push(line.slice(blockIndent));
      continue;
    }
    if (key !== null && blockMode !== null && blockIndent === 0) {
      // First content line of the block scalar fixes the common indent.
      blockIndent = indent;
      blockLines.push(line.slice(indent));
      continue;
    }
    if (key !== null && listItems !== null) {
      const li = line.match(/^\s+-\s+(.*)$/);
      if (li) {
        listItems.push(li[1]);
        continue;
      }
      throw new Error(`${filename}: front-matter line ${i + 1} is not a list item under \`${key}\`: ${line.trim().slice(0, 60)}`);
    }
    const match = line.match(/^([a-z_]+):\s*(.*)$/);
    if (!match) {
      throw new Error(`${filename}: cannot parse front-matter line ${i + 1}: ${line.trim().slice(0, 60)}`);
    }
    flush();
    key = match[1];
    const rest = match[2];
    if (rest === "|" || rest === ">") {
      blockMode = rest;
      blockIndent = 0;
      blockLines = [];
    } else if (rest === "") {
      listItems = [];
    } else {
      data[key] = /^\d+$/.test(rest) ? Number(rest) : stripQuotes(rest);
      key = null;
    }
  }
  flush();

  const body = lines.slice(cursor + 1).join("\n").replace(/^\n+/, "").replace(/\s+$/, "");
  return { data, body };
}

export async function readCatalogPage(filename) {
  const raw = await readFile(new URL(filename, CATALOG_DIR), "utf8");
  const { data, body } = parseFrontMatter(raw, filename);
  const page = { ...data, _body: body, _filename: filename };
  return page;
}

// Validates one parsed page; returns an array of issue strings (empty = OK).
export function validatePage(page) {
  const issues = [];
  const { _filename: filename, ...data } = page;
  for (const field of REQUIRED_FIELDS) {
    if (data[field] === undefined || data[field] === "") {
      issues.push(`${filename}: missing required front-matter field \`${field}\``);
    }
  }
  if (typeof data.status !== "number") {
    issues.push(`${filename}: \`status\` must be a plain integer (got ${JSON.stringify(data.status)})`);
  } else if (data.status < 400 || data.status > 599) {
    issues.push(`${filename}: \`status\` must be an HTTP error status (got ${data.status})`);
  }
  const expectedName = `${data.code}.md`;
  if (filename !== expectedName) {
    issues.push(`${filename}: front-matter \`code\` ${data.code} does not match file name (expected ${expectedName})`);
  }
  if (data.type !== undefined && data.type !== DOCS_BASE + data.code) {
    issues.push(`${filename}: \`type\` must be ${DOCS_BASE + data.code}`);
  }
  return issues;
}

// --- markdown → HTML (subset used by page bodies and action fields) --------

function escapeHtml(text) {
  return text.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

function renderInline(text) {
  let html = escapeHtml(text);
  html = html.replace(/`([^`]+)`/g, "<code>$1</code>");
  html = html.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  html = html.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, '<a href="$2">$1</a>');
  return html;
}

export function renderMarkdown(md) {
  if (!md) return "";
  const lines = md.split("\n");
  const out = [];
  let paragraph = [];
  let list = null;
  let fence = null;

  const closeParagraph = () => {
    if (paragraph.length > 0) {
      out.push(`<p>${renderInline(paragraph.join(" "))}</p>`);
      paragraph = [];
    }
  };
  const closeList = () => {
    if (list !== null) {
      out.push(`<ul>${list.map((item) => `<li>${renderInline(item)}</li>`).join("")}</ul>`);
      list = null;
    }
  };

  for (const line of lines) {
    if (fence !== null) {
      if (line.trim() === "```") {
        out.push(`<pre><code>${escapeHtml(fence.join("\n"))}</code></pre>`);
        fence = null;
      } else {
        fence.push(line);
      }
      continue;
    }
    if (line.trim() === "```") {
      closeParagraph();
      closeList();
      fence = [];
      continue;
    }
    if (line.trim() === "") {
      closeParagraph();
      closeList();
      continue;
    }
    const heading = line.match(/^(#{1,4})\s+(.*)$/);
    if (heading) {
      closeParagraph();
      closeList();
      const level = heading[1].length + 2; // ## → h4 inside a page already titled h1
      out.push(`<h${level}>${renderInline(heading[2])}</h${level}>`);
      continue;
    }
    const item = line.match(/^\s*[-*]\s+(.*)$/);
    if (item) {
      closeParagraph();
      list ??= [];
      list.push(item[1]);
      continue;
    }
    // Indented continuation of the previous list item.
    if (list !== null && /^\s+\S/.test(line)) {
      list[list.length - 1] += ` ${line.trim()}`;
      continue;
    }
    paragraph.push(line.trim());
  }
  closeParagraph();
  closeList();
  if (fence !== null) out.push(`<pre><code>${escapeHtml(fence.join("\n"))}</code></pre>`);
  return out.join("\n");
}

// --- skeleton generation ----------------------------------------------------

export function skeletonPage(code, status, { operator = false } = {}) {
  const def = STATUS_DEFAULTS.get(status) ?? STATUS_DEFAULTS.get(500);
  const lines = [
    "---",
    `code: ${code}`,
    `status: ${status}`,
    `title: ${deriveTitle(code)}`,
    "summary: |",
    ...def.summary.split("\n").map((l) => `  ${l}`),
    "client_action: |",
    ...def.client.split("\n").map((l) => `  ${l}`),
    "operator_action: |",
    ...def.operator.split("\n").map((l) => `  ${l}`),
    "---",
    "",
    "<!-- Skeleton generated by check-problem-catalog.mjs --write-skeletons.",
     `     Add related_routes and tighten the wording for this code. -->`,
    "",
  ];
  return lines.join("\n");
}
