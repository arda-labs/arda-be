# Code Mode and Meta-Tools Architecture

## Overview and Motivation

As the Arda ecosystem expands to cover multiple business domains (CRM, HRM, Finance, Supply Chain, Workflow, MDM), exposing each microservice endpoint as a separate JSON tool definition to the LLM presents three major scalability bottlenecks:

1. **Context Window Inflation:** Registering 50–100+ individual tool schemas requires 10,000–30,000+ input tokens per conversational turn before the user prompt is even processed.
2. **Multi-Turn Latency Bottleneck:** Complex business queries (e.g., "Find the top 5 overdue enterprise customers and calculate their total pending invoice balance") require sequential tool calling with 10–15 round-trips between the model and server, causing unacceptable latency (15–30+ seconds).
3. **Tool Selection Degradation:** As tool counts grow, LLMs suffer from increased hallucination and parameter mismatch when choosing among dozens of similar schemas.

To resolve these challenges, Arda adopts the **2 Meta-Tools (Code Mode)** pattern inspired by Cloudflare Agents and the Model Context Protocol (MCP):

```text
Traditional Sequential Tools:
  User -> Model -> Tool 1 -> Model -> Tool 2 -> Model -> Tool 3 -> Model -> Response
  (3-5 LLM round-trips, massive prompt overhead, high latency)

Code Mode (Search & Execute):
  User -> Model -> search("crm invoice finance") -> Model -> execute(js_script) -> Response
  (1-2 LLM round-trips, 95%+ token reduction, multi-step operations run in sandbox at native speed)
```

---

## The 2 Meta-Tools Contract

The AI agent in `ai-service` is equipped with two primary meta-tools:

### 1. `search` (Dynamic SDK Discovery)

Allows the model to explore available APIs and their TypeScript/JSDoc signatures on-demand.

```json
{
  "name": "search",
  "version": 1,
  "kind": "read",
  "description": "Discover available Arda domain SDK methods, parameters, and documentation by keyword or domain filter",
  "parameters": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Keywords describing the required action or domain entity (e.g. 'crm customer search', 'hrm employee leaves', 'finance invoice summary')"
      },
      "domain": {
        "type": "string",
        "enum": ["crm", "hrm", "finance", "workflow", "knowledge", "all"],
        "description": "Optional domain filter to narrow search scope"
      }
    },
    "required": ["query"]
  }
}
```

**Output format:** Returns concise TypeScript method signatures and JSDoc explanations for matching methods:
```typescript
/**
 * Read customer details by identifier in the active tenant.
 * @requires crm.customer.read
 */
arda.crm.getCustomer(args: { customerId: string }): Promise<CustomerSummary>;

/**
 * Query customer invoices matching status and date ranges.
 * @requires finance.invoice.read
 */
arda.finance.listInvoices(args: { customerId?: string, status?: string, limit?: number }): Promise<InvoiceListResult>;
```

### 2. `execute` (Sandboxed Script Execution)

Executes a JavaScript program written by the LLM that composes calls to the injected `arda.*` SDK.

```json
{
  "name": "execute",
  "version": 1,
  "kind": "read",
  "description": "Execute sandboxed JavaScript code against the arda.* SDK to query, aggregate, filter, or propose actions across Arda domain services",
  "parameters": {
    "type": "object",
    "properties": {
      "code": {
        "type": "string",
        "description": "JavaScript (ES6) code to execute. Can use await with arda.* SDK methods, array transformations (map, filter, reduce), and console.log for debugging. Must return the final result."
      }
    },
    "required": ["code"]
  }
}
```

---

## Sandbox Architecture and Security Invariants

### 1. Embedded Sandbox Engine (Pure Go `goja`)

To avoid deploying and managing separate Node.js worker pools, `ai-service` runs the sandboxed script using **`goja`** (pure Go ECMAScript runtime):
- **Near-zero startup cost:** Sub-millisecond VM instantiation per execution turn.
- **Strict Isolation:** The sandbox starts with a stripped global scope. Globals like `eval`, `Function`, `Reflect`, `process`, `require`, and `window` are removed or disabled.
- **Zero OS/FS/Net access:** No filesystem or arbitrary TCP/HTTP network access is permitted within the script. The only external bridge is the injected `arda.*` SDK namespace.

**Hard Resource Quotas:**

| Limit | Default | Max configurable |
| :--- | :--- | :--- |
| Execution timeout | 3 000 ms | 5 000 ms |
| Script source size | 16 KiB | — |
| Result output size | 64 KiB | — |
| Peak memory per VM | 32 MiB | — |
| Concurrent sandbox VMs | 8 per pod | — |

Timeout is enforced via `vm.Interrupt()` after the deadline. A script that
exceeds any quota is terminated immediately and returns an `execute` tool result
with `error: "sandbox_quota_exceeded"`.

### 2. Script Validation (Pre-execution Static Checks)

Before handing the script to the Goja VM, `ai-service` performs static analysis
to reject obviously dangerous patterns without paying VM startup cost:

- **Forbidden identifiers:** Reject scripts containing `eval`, `new Function`,
  `__proto__`, `constructor[`, `Object.defineProperty`, `Proxy`, `Reflect`,
  `globalThis`, and `import(`.
- **Size gate:** Reject scripts larger than 16 KiB.
- **Encoding check:** Reject non-UTF-8 or null-byte input.

Static rejection returns HTTP 422 with code `ai.sandbox_script_rejected` and
a brief reason (e.g., `"forbidden_identifier: eval"`). The rejection is logged
and counted as a failed tool execution.

### 3. Multi-Tenancy & Authorization Invariants

1. **Zero Client-Supplied Trust:** The script generated by the LLM is **strictly
   forbidden from supplying `tenantId` or `userId`**. All downstream requests
   inherit the server-resolved `tools.Context` (derived from Gateway headers
   `X-Tenant-Id`, `X-User-Id`, `X-Permissions`).
2. **Method-Level Permission Check:** Every method in the `arda.*` SDK validates
   that `tools.Context.Permissions` contains the required permission (e.g.,
   `crm.customer.read`) before executing. If missing, the SDK method throws a
   catchable `PermissionDenied` error inside the sandbox (see SDK Error Contract
   below).
3. **Data Redaction & Bounded Payloads:** Results returned from domain
   microservices are redacted and bounded before being returned to the script
   environment.

### 4. SDK Error Contract

Every `arda.*` method in the sandbox throws a structured JS error on failure so
scripts can catch and handle errors gracefully:

```typescript
// Error thrown inside the sandbox on failure
class ArdaSDKError extends Error {
  code: string;     // "permission_denied" | "not_found" | "timeout" | "unavailable" | "approval_required"
  domain: string;   // e.g. "crm", "hrm", "finance"
  method: string;   // e.g. "crm.getCustomer"
  requestId: string;
}
```

Scripts should handle these explicitly:
```javascript
try {
  const customer = await arda.crm.getCustomer({ customerId: "C-001" });
  return customer;
} catch (e) {
  if (e.code === "not_found") return { result: null, reason: "customer_not_found" };
  throw e; // re-throw unknown errors — sandbox will surface them as execute errors
}
```

A thrown `ArdaSDKError` with `code: "approval_required"` means a mutation method
was called. The sandbox terminates, and the `execute` tool result contains the
proposal record.

### 5. Handling Mutations and Human-In-The-Loop (HITL)

The sandbox supports both read-only aggregation and high-risk action preparation:

- **Read Operations (`kind: "read"`):** Execute immediately and return data to the
  script (e.g., `arda.crm.getCustomer`, `arda.knowledge.search`).
- **Mutation Operations (`kind: "confirm"`):**
  - When an `arda.*` mutation method is called (e.g., `arda.crm.exportCustomer(args)`),
    the SDK method **does not perform the side effect directly**.
  - Instead, the SDK generates an `ApprovalProposal` record in `ai_approval_proposals`
    and throws `ArdaSDKError { code: "approval_required" }` into the script.
  - The sandbox execution terminates; the `execute` tool result contains the
    proposal record with `status: "WAITING_APPROVAL"`.
  - The HITL resume flow is identical to direct confirm-kind tools.

### 6. Sandbox Observability

Every `execute` invocation records the following in `ai_tool_executions`:

- `script_hash`: SHA-256 of the script source (for deduplication and anomaly detection).
- `sdk_methods_called`: ordered list of `arda.*` method names invoked (no argument values).
- `execution_duration_ms`: wall-clock time from VM start to result.
- `quota_exceeded`: boolean + which quota was hit.
- `static_rejected`: boolean + rejected pattern if pre-execution check fired.
- `status`: `SUCCEEDED | FAILED | QUOTA_EXCEEDED | STATIC_REJECTED | APPROVAL_REQUIRED`.
- `error_code`: structured error code on failure.

Raw script source is **never stored in the model transcript**. The script hash
is stored as a bounded reference for audit correlation.

---

## SDK Catalog Generation from Contracts

To eliminate manual tool writing for each microservice endpoint:

1. Domain services define their APIs via OpenAPI specifications in `contracts/openapi/` or Protocol Buffers in `proto/`.
2. Endpoints tagged with `@ai-tool` are parsed by an SDK Catalog generator.
3. The generator outputs:
   - **TypeScript Definition Catalog:** Indexed by keyword/domain for the `search` meta-tool.
   - **Go Binding Dispatchers:** Automatically binds HTTP/gRPC client calls with tenant context injection into the Goja runtime for `execute`.

---

## Target Request Flow

```text
1. User asks: "Show me all high-risk customers updated in the last 7 days and their account segments."
2. ai-service runs Agent Loop with 2 tools: search, execute.
3. Turn 1:
   LLM calls: search({ query: "customer risk level segment updated" })
   search returns:
     arda.crm.searchCustomers({ riskLevel?: string, updatedAfter?: string }): Promise<CustomerSummary[]>
4. Turn 2:
   LLM calls: execute({
     code: `
       const oneWeekAgo = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString();
       const customers = await arda.crm.searchCustomers({ riskLevel: "high", updatedAfter: oneWeekAgo });
       return customers.map(c => ({ id: c.id, name: c.name, segment: c.segment, rank: c.rank }));
     `
   })
5. Sandbox runs script -> Injects Tenant context -> Calls CRM Service -> Returns filtered JSON array.
6. Turn 3:
   LLM receives compact JSON and formats final markdown response with table to user.
```

---

## Migration and Rollout Strategy

1. **Phase 1 (Current State):** Direct Go Tool Registry with `crm.customer.get`, `knowledge.search`, and `crm.customer.export.prepare`.
2. **Phase 2 (Sandbox POC):** Introduce `goja` sandbox engine in `internal/sandbox/`, implement `execute` meta-tool with bindings to existing `crm` and `knowledge` tools.
3. **Phase 3 (Catalog & Search):** Implement `search` meta-tool with indexed SDK definitions; transition default agent loop from direct tools to `search` + `execute`.
4. **Phase 4 (Enterprise Domain Expansion):** Integrate OpenAPI generator to dynamically bind HRM, Finance, and Workflow services into the `arda.*` SDK.
