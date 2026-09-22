import { readdirSync, readFileSync, statSync } from "node:fs"
import { join, relative, resolve } from "node:path"
import { fileURLToPath } from "node:url"

/**
 * JSON field-name gate — public REST JSON uses snake_case (see
 * docs/conventions/http-api.md and docs/refactor-program/02-contracts-and-frontend.md).
 *
 * camelCase json tags are only allowed for:
 *  - protocol-owned payloads (AG-UI events consumed verbatim by the browser UI);
 *  - the documented browser auth boundary (`UserContext` / BFF session / `/api/auth/me`).
 *
 * Everything else is either a violation or a dated LEGACY_BASELINE entry that
 * must disappear with the snake_case migration wave.
 *
 * Run with --report to print every offender without failing.
 * Run with --baseline to print ready-to-paste LEGACY_BASELINE entries.
 */

const PROTOCOL_ALLOWLIST = new Map([
  [
    "apps/ai-service/internal/events/events.go#",
    "AG-UI protocol events (browser-consumed verbatim)",
  ],
  [
    "apps/ai-service/internal/handler/sse.go#agUiEvent",
    "AG-UI SSE envelope (protocol)",
  ],
  [
    "apps/ai-service/internal/handler/router.go#runInput",
    "AG-UI RunAgentInput (protocol)",
  ],
  [
    "apps/ai-service/internal/handler/router.go#agentEvent",
    "AG-UI agent events (protocol)",
  ],
  [
    "apps/ai-service/internal/handler/router.go#agUiResumeEntry",
    "AG-UI resume entry (protocol)",
  ],
  [
    "apps/iam-service/internal/domain/user.go#UserContext",
    "documented browser auth boundary (/api/auth/me, BFF session)",
  ],
  [
    "apps/iam-service/internal/domain/tenant.go#TenantMembership",
    "nested in UserContext (browser auth boundary)",
  ],
  [
    "apps/auth-gateway/internal/iamclient/client.go#UserContext",
    "mirrors the iam UserContext browser contract",
  ],
  [
    "apps/auth-gateway/internal/iamclient/client.go#TenantMembership",
    "mirrors the iam UserContext browser contract",
  ],
  [
    "apps/auth-gateway/internal/session/session.go#UserInfo",
    "BFF session payload (browser auth boundary)",
  ],
  [
    "apps/auth-gateway/internal/session/session.go#TenantMembership",
    "nested in the BFF session payload (browser auth boundary)",
  ],
])

// Historical structs. Remove an entry in the same PR that renames its tags.
const LEGACY_BASELINE = new Map([
  ["apps/ai-service/internal/handler/profiles.go#applyModelRequest", "Q2-2027"],
  ["apps/ai-service/internal/handler/profiles.go#profileDTO", "Q2-2027"],
  ["apps/ai-service/internal/handler/profiles.go#profileModelDTO", "Q2-2027"],
  ["apps/ai-service/internal/handler/profiles.go#profileUpsertRequest", "Q2-2027"],
  ["apps/ai-service/internal/handler/quota_handler.go#QuotasResponseDTO", "Q2-2027"],
  ["apps/ai-service/internal/handler/quota_handler.go#UpdateQuotasRequestDTO", "Q2-2027"],
  ["apps/ai-service/internal/handler/router.go#CatalogToolDTO", "Q2-2027"],
  ["apps/ai-service/internal/handler/router.go#approvalProposalInput", "Q2-2027"],
  ["apps/ai-service/internal/handler/router.go#updateToolRequest", "Q2-2027"],
  ["apps/ai-service/internal/handler/settings.go#settingsDTO", "Q2-2027"],
  ["apps/ai-service/internal/handler/settings.go#testConnectionRequest", "Q2-2027"],
  ["apps/ai-service/internal/handler/settings.go#testConnectionResponse", "Q2-2027"],
  ["apps/ai-service/internal/repository/approval_store.go#ApprovalDetail", "Q2-2027"],
  ["apps/ai-service/internal/repository/approval_store.go#ApprovalRecord", "Q2-2027"],
  ["apps/ai-service/internal/repository/profile_store.go#AIModelProfile", "Q2-2027"],
  ["apps/ai-service/internal/repository/profile_store.go#AIModelProfileModel", "Q2-2027"],
  ["apps/ai-service/internal/repository/quota_store.go#QuotaSettings", "Q2-2027"],
  ["apps/ai-service/internal/repository/run_store.go#AnalyticsSummary", "Q2-2027"],
  ["apps/ai-service/internal/repository/run_store.go#ConversationMessage", "Q2-2027"],
  ["apps/ai-service/internal/repository/run_store.go#ConversationSummary", "Q2-2027"],
  ["apps/ai-service/internal/repository/run_store.go#FeedbackStats", "Q2-2027"],
  ["apps/ai-service/internal/repository/run_store.go#LatencyStats", "Q2-2027"],
  ["apps/ai-service/internal/repository/run_store.go#ModelUsage", "Q2-2027"],
  ["apps/ai-service/internal/repository/run_store.go#RAGQualityStats", "Q2-2027"],
  ["apps/ai-service/internal/repository/tenant_settings.go#TenantSettings", "Q2-2027"],
  ["apps/ai-service/internal/repository/tool_settings.go#ToolSetting", "Q2-2027"],
  ["apps/ai-service/internal/sandbox/engine.go#ExecutionResult", "Q2-2027"],
  ["apps/ai-service/internal/sandbox/resultstore.go#storedResult", "Q2-2027"],
  ["apps/ai-service/internal/tools/meta_read.go#readResultArguments", "Q2-2027"],
  ["apps/ai-service/internal/tools/types.go#ApprovalPending", "Q2-2027"],
  ["apps/crm-service/internal/repository/amendment_repository.go#AmendmentUpsert", "Q2-2027"],
  ["apps/crm-service/internal/repository/amendment_repository.go#CustomerAmendment", "Q2-2027"],
  ["apps/crm-service/internal/repository/customer_repository.go#Customer", "Q2-2027"],
  ["apps/crm-service/internal/repository/customer_repository.go#CustomerRelationship", "Q2-2027"],
  ["apps/crm-service/internal/repository/customer_repository.go#CustomerRelationshipCreate", "Q2-2027"],
  ["apps/crm-service/internal/repository/customer_repository.go#CustomerUpsert", "Q2-2027"],
  ["apps/finance-service/internal/domain/coa.go#AccClassCoaMap", "Q2-2027"],
  ["apps/finance-service/internal/domain/coa.go#AccStructure", "Q2-2027"],
  ["apps/finance-service/internal/domain/coa.go#AccStructureSegment", "Q2-2027"],
  ["apps/finance-service/internal/domain/coa.go#CoaAccount", "Q2-2027"],
  ["apps/finance-service/internal/domain/coa.go#CoaVersion", "Q2-2027"],
  ["apps/finance-service/internal/domain/coa.go#ResolvedCoaAccount", "Q2-2027"],
  ["apps/finance-service/internal/domain/finance.go#Account", "Q2-2027"],
  ["apps/finance-service/internal/domain/finance.go#AccountClassification", "Q2-2027"],
  ["apps/finance-service/internal/domain/finance.go#JournalDefinition", "Q2-2027"],
  ["apps/finance-service/internal/domain/finance.go#JournalLine", "Q2-2027"],
  ["apps/finance-service/internal/domain/finance.go#NamedAccountMapping", "Q2-2027"],
  ["apps/finance-service/internal/domain/finance.go#ProcessConfig", "Q2-2027"],
  ["apps/hrm-service/internal/handler/internal_ai_handler.go#aiEmployee", "Q2-2027"],
  ["apps/iam-service/internal/domain/tenant.go#TenantMember", "Q2-2027"],
  ["apps/iam-service/internal/handler/internal_dto.go#resolveIdentityRequest", "Q2-2027"],
  ["apps/iam-service/internal/handler/internal_dto.go#resolveKratosIdentityRequest", "Q2-2027"],
  ["apps/iam-service/internal/repository/audit_repo.go#AuditStats", "Q2-2027"],
  ["apps/iam-service/internal/repository/user_repository.go#IdentityConsistencyIssue", "Q2-2027"],
  ["apps/iam-service/internal/service/mfa_service.go#MFAResult", "Q2-2027"],
  ["apps/loan-service/internal/service/batch_collection_service.go#batchCollectionRowVars", "Q2-2027"],
  ["apps/loan-service/internal/service/batch_disbursement_service.go#batchRowVars", "Q2-2027"],
  ["apps/notification-service/internal/handler/internal_ai_handler.go#aiInboxItem", "Q2-2027"],
  ["apps/platform-service/internal/handler/internal_ai_handler.go#aiLookupValue", "Q2-2027"],
  ["apps/platform-service/internal/handler/internal_ai_handler.go#aiOrganization", "Q2-2027"],
  ["apps/platform-service/internal/handler/internal_ai_handler.go#aiParameter", "Q2-2027"],
  ["apps/workflow-service/internal/handler/case_monitor.go#CaseMonitorView", "Q2-2027"],
  ["apps/workflow-service/internal/handler/internal_ai_handler.go#aiBusinessCase", "Q2-2027"],
  ["apps/workflow-service/internal/handler/internal_ai_handler.go#aiTimelineEvent", "Q2-2027"],
  ["apps/workflow-service/internal/handler/internal_ai_handler.go#aiWorkItem", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate.go#OperateElementStat", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate.go#OperateIncident", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate.go#OperateJob", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate.go#OperateJobDefinition", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate.go#OperateProcessDef", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate.go#OperateProcessInstance", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateIncidentPage", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateIncidentRow", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateInstanceDetail", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateInstancePage", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateInstanceRow", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateJobPage", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateJobRow", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateSummary", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateUserTaskPage", "Q2-2027"],
  ["apps/workflow-service/internal/handler/operate_runtime.go#operateUserTaskRow", "Q2-2027"],
  ["apps/workflow-service/internal/handler/workflow_handler.go#MessageRequest", "Q2-2027"],
  ["apps/workflow-service/internal/handler/workflow_handler.go#StartRequest", "Q2-2027"],
  ["apps/workflow-service/internal/handler/workflow_handler.go#caseActorRequest", "Q2-2027"],
  ["apps/workflow-service/internal/repository/case_repository.go#BusinessCase", "Q2-2027"],
  ["apps/workflow-service/internal/repository/case_repository.go#CaseCreate", "Q2-2027"],
  ["apps/workflow-service/internal/repository/case_repository.go#CaseType", "Q2-2027"],
  ["apps/workflow-service/internal/repository/case_repository.go#CaseTypeUpsert", "Q2-2027"],
  ["apps/workflow-service/internal/repository/case_repository.go#TimelineEvent", "Q2-2027"],
  ["apps/workflow-service/internal/repository/config_repository.go#DescriptionTemplate", "Q2-2027"],
  ["apps/workflow-service/internal/repository/config_repository.go#ProcessConfigUpdate", "Q2-2027"],
  ["apps/workflow-service/internal/repository/config_repository.go#ProcessRole", "Q2-2027"],
  ["apps/workflow-service/internal/repository/config_repository.go#SLAPolicy", "Q2-2027"],
  ["apps/workflow-service/internal/repository/config_repository.go#SLATaskPolicy", "Q2-2027"],
  ["apps/workflow-service/internal/repository/process_definition_repository.go#ProcessDefinition", "Q2-2027"],
  ["apps/workflow-service/internal/repository/role_management_repository.go#WorkflowAssignmentRule", "Q2-2027"],
  ["apps/workflow-service/internal/repository/role_management_repository.go#WorkflowDelegation", "Q2-2027"],
  ["apps/workflow-service/internal/repository/role_management_repository.go#WorkflowRoleCatalog", "Q2-2027"],
  ["apps/workflow-service/internal/repository/role_management_repository.go#WorkflowRoleMembership", "Q2-2027"],
  ["apps/workflow-service/internal/repository/work_item_repository.go#WorkItem", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_incident_index.go#ZeebeIncident", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_incident_index.go#esIncidentRecord", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_incident_index.go#esIncidentValue", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#ZeebeElementInstance", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#ZeebeHistoryEvent", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#ZeebeJob", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#ZeebeProcessInstance", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#ZeebeVariable", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#esHistoryRecord", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#esIncidentCompositeResponse", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#esJobCompositeResponse", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#esJobRecord", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#esPICompositeResponse", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#esProcessInstanceRecord", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#esUserTaskCompositeResponse", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_monitoring_index.go#esVariableRecord", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_rest.go#ZeebeUserTask", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_service.go#ProcessIncidentSnapshot", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_service.go#ProcessJobSnapshot", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_service.go#WorkflowTask", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_user_task_index.go#esUserTaskRecord", "Q2-2027"],
  ["apps/workflow-service/internal/service/zeebe_user_task_index.go#esUserTaskValue", "Q2-2027"],
])

const isReport = process.argv.includes("--report")
const isBaseline = process.argv.includes("--baseline")
const root = resolve(fileURLToPath(new URL("..", import.meta.url)))

const violations = []
const cleaned = []
const offenders = []
let structs = 0
let camelStructs = 0
let camelTags = 0
let snakeTags = 0

// HTTP-body decode targets must declare json tags. This is a separate invariant
// from camelCase-vs-snake_case: a request struct with NO tags at all passes the
// camel check silently, yet encoding/json cannot then bind a snake_case body,
// so every call fails at the handler ("customer_code is required"). That
// happened to the CRM member register/capital payloads, which only surfaced
// when the endpoint was driven for real.
// structTypeNames: "path#Type" -> number of json tags declared.
const structTagCount = new Map()
// decodeTargets: count of HTTP-body decode sites inspected.
let decodeTargets = 0

for (const file of walk(join(root, "apps"))) {
  if (!file.endsWith(".go") || file.endsWith("_test.go")) continue
  const rel = relative(root, file).replaceAll("\\", "/")
  const lines = readFileSync(file, "utf8").split("\n")
  let current = null
  let tags = []
  const flush = () => {
    if (!current) return
    structs += 1
    structTagCount.set(`${rel}#${current}`, tags.length)
    const camel = tags.filter((name) => /^[a-z][a-zA-Z0-9]*[A-Z]/.test(name))
    camelTags += camel.length
    snakeTags += tags.length - camel.length
    if (camel.length > 0) {
      camelStructs += 1
      const key = `${rel}#${current}`
      const allowKey = [...PROTOCOL_ALLOWLIST.keys()].find((entry) =>
        key.startsWith(entry)
      )
      if (allowKey) {
        // protocol / documented exception
      } else if (LEGACY_BASELINE.has(key)) {
        offenders.push({ key, camel: camel.length, names: camel, legacy: true })
      } else {
        violations.push(
          `${key}: ${camel.length} camelCase json tags (${camel.slice(0, 4).join(", ")}${camel.length > 4 ? ", …" : ""})`
        )
        offenders.push({ key, camel: camel.length, names: camel, legacy: false })
      }
    } else if (LEGACY_BASELINE.has(`${rel}#${current}`)) {
      cleaned.push(`${rel}#${current}`)
    }
    current = null
    tags = []
  }
  for (const line of lines) {
    const open = line.match(/^type (\w+) struct \{/)
    if (open) {
      flush()
      current = open[1]
      continue
    }
    if (current && /^\}/.test(line)) {
      flush()
      continue
    }
    if (!current) continue
    const tag = line.match(/json:"([^"]+)"/)
    if (!tag) continue
    const name = tag[1].split(",")[0]
    if (!name || name === "-") continue
    tags.push(name)
  }
  flush()
}

// Second pass: find HTTP-body decode targets and require json tags on the
// struct type they name. The target type is the nearest preceding
// `var <name> <Type>` for the decoded variable (variables like `in` are reused
// across handlers, so a whole-file map would resolve to the wrong type).
const untaggedDecodeTargets = []
// Struct names are not globally unique (e.g. two `Organization` types, one an
// internal read model with no tags). Index by leaf name but keep every tag
// count seen, and only flag a decode target when ALL same-named structs are
// untagged — an ambiguous name with a tagged twin is not this gate's business.
const tagCountsByTypeName = new Map()
for (const key of structTagCount.keys()) {
  const leaf = key.split("#")[1]
  const list = tagCountsByTypeName.get(leaf) ?? []
  list.push(structTagCount.get(key))
  tagCountsByTypeName.set(leaf, list)
}
for (const file of walk(join(root, "apps"))) {
  if (!file.endsWith(".go") || file.endsWith("_test.go")) continue
  const rel = relative(root, file).replaceAll("\\", "/")
  const src = readFileSync(file, "utf8")
  if (!/json\.NewDecoder\(r\.Body\)\.Decode\(&/.test(src)) continue
  for (const m of src.matchAll(/json\.NewDecoder\(r\.Body\)\.Decode\(&(\w+)\)/g)) {
    const name = m[1]
    const before = src.slice(0, m.index)
    // nearest `var <name> <Type>` above the decode
    const declRe = new RegExp(`var\\s+${name}\\s+([A-Za-z_][\\w.]*)`, "g")
    let ref = null
    let d
    while ((d = declRe.exec(before))) ref = d[1]
    if (!ref) continue
    const leaf = ref.includes(".") ? ref.split(".").pop() : ref
    if (leaf === "struct" || leaf === "map") continue
    const counts = tagCountsByTypeName.get(leaf)
    if (!counts) continue
    decodeTargets += 1
    if (counts.every((c) => c === 0)) untaggedDecodeTargets.push(`${rel} -> ${leaf}`)
  }
}

if (cleaned.length > 0) {
  console.log(
    "Baseline cleanups ready (remove from LEGACY_BASELINE):",
    cleaned.join(", ")
  )
}

if (isBaseline) {
  const pending = offenders
    .filter((item) => !item.legacy)
    .map((item) => item.key)
    .sort()
  console.log("const LEGACY_BASELINE = new Map([")
  for (const key of pending) {
    console.log(`  ["${key}", "Q2-2027"],`)
  }
  console.log("])")
  process.exit(0)
}

if (isReport) {
  offenders.sort((a, b) => b.camel - a.camel)
  for (const item of offenders) {
    console.log(
      `${item.legacy ? "[baseline]" : "[violation]"} ${item.camel.toString().padStart(3)}  ${item.key}`
    )
    if (!item.legacy && item.camel <= 4) {
      console.log(`              ${item.names.join(", ")}`)
    }
  }
}

if (violations.length > 0) {
  const message = [
    ...violations,
    "",
    "snake_case is the public REST convention (arda-be/docs/conventions/http-api.md).",
    isReport
      ? "(report mode — add legitimate protocol exceptions to PROTOCOL_ALLOWLIST or dated entries to LEGACY_BASELINE)"
      : "Rename the tags or register a dated LEGACY_BASELINE entry (team decision).",
  ].join("\n")
  if (isReport) {
    console.warn(message)
  } else {
    console.error(message)
    process.exit(1)
  }
}

if (untaggedDecodeTargets.length > 0) {
  const message = [
    "HTTP request structs decoded from the body must declare json tags:",
    ...[...new Set(untaggedDecodeTargets)].sort().map((t) => `  ${t}`),
    "",
    "A body struct with no tags cannot bind a snake_case payload, so every call",
    "fails at the handler. Tag each field, e.g. CustomerCode string `json:\"customer_code\"`.",
  ].join("\n")
  if (isReport) {
    console.warn(message)
  } else {
    console.error(message)
    process.exit(1)
  }
}

console.log(
  `JSON tag invariant OK (${structs} structs, ${snakeTags} snake | ${camelTags} camel tags; ${camelStructs} structs with camel, ${PROTOCOL_ALLOWLIST.size} protocol exception entries; ${decodeTargets} decode targets)`
)

function* walk(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (["node_modules", ".git", "vendor"].includes(entry.name)) continue
    const full = join(dir, entry.name)
    if (entry.isDirectory()) yield* walk(full)
    else if (statSync(full).isFile()) yield full
  }
}
