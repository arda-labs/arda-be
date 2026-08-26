# SDK Catalog Design — `search` Meta-Tool

Status: **Design specification — required before `search` meta-tool implementation**.
Defines how the `arda.*` TypeScript SDK catalog is built, indexed, served, and
kept in sync with domain service contracts.

---

## 1. Overview

The `search(query, domain?)` meta-tool allows the model to discover available
SDK methods on-demand without loading all schemas into the prompt context. This
requires a catalog that is:

- **Fast to query** (< 50 ms per search, in-process)
- **Automatically synchronized** with domain service OpenAPI contracts
- **Structured for LLM consumption** (TypeScript signatures + JSDoc, not raw JSON)
- **Permission-aware** (only expose methods the current user's roles could use)

---

## 2. Catalog Structure

### 2.1 Catalog Entry Schema

Each entry in the catalog represents one `arda.*` SDK method:

```go
// internal/catalog/entry.go
type CatalogEntry struct {
    // Stable identifier used to locate the Go dispatcher
    MethodName  string   // e.g. "crm.getCustomer"
    // Dot-namespaced accessor on the arda.* object
    SDKPath     string   // e.g. "arda.crm.getCustomer"
    Domain      string   // "crm" | "hrm" | "finance" | "workflow" | "knowledge"

    // What the model receives from search()
    Signature   string   // TypeScript function signature
    JSDoc       string   // Description + @param + @returns + @requires
    Keywords    []string // Indexed terms for BM25 scoring

    // Server-side enforcement metadata (not sent to model)
    Kind              string   // "read" | "confirm"
    RequiredPerms     []string // ["crm.customer.read"]
    Risk              string   // "low" | "medium" | "high"
    DownstreamService string   // "crm-service"
    DownstreamPath    string   // "/api/crm/customers/{customerId}"
    TimeoutMs         int
}
```

### 2.2 Catalog Output Format (what `search` returns to the model)

```typescript
/**
 * Read a redacted customer summary in the active tenant.
 * Returns name, code, status, segment, rank, and risk level.
 * @param args.customerId  Arda customer identifier or customer code (max 128 chars)
 * @returns CustomerSummary
 * @requires crm.customer.read
 * @domain crm
 */
arda.crm.getCustomer(args: { customerId: string }): Promise<CustomerSummary>;

/**
 * Search published knowledge sources with cited results.
 * @param args.query  Natural language search query (max 512 chars)
 * @param args.limit  Number of results, 1–5 (default 3)
 * @returns KnowledgeSearchResult[]
 * @requires ai.knowledge.read
 * @domain knowledge
 */
arda.knowledge.search(args: { query: string; limit?: number }): Promise<KnowledgeSearchResult[]>;
```

---

## 3. Build Pipeline

### 3.1 Source of Truth: OpenAPI Contracts + Manual Registry

Catalog entries come from two sources:

**Source A — OpenAPI annotations** (automated, scalable):

Domain service OpenAPI specs in `contracts/openapi/` are annotated with an
`x-ai-tool` extension:

```json
{
  "paths": {
    "/api/crm/customers/{customerId}": {
      "get": {
        "x-ai-tool": {
          "sdkPath": "arda.crm.getCustomer",
          "domain": "crm",
          "kind": "read",
          "keywords": ["customer", "client", "account", "code", "name", "status", "segment", "risk"],
          "requiredPerms": ["crm.customer.read"],
          "risk": "low",
          "timeoutMs": 3000
        },
        "summary": "Read a redacted customer summary in the active tenant."
      }
    }
  }
}
```

**Source B — Manual Go registry** (for tools without OpenAPI specs or with
complex multi-endpoint orchestration):

```go
// internal/catalog/manual.go
func ManualEntries() []CatalogEntry {
    return []CatalogEntry{
        {
            MethodName: "knowledge.search",
            SDKPath:    "arda.knowledge.search",
            Domain:     "knowledge",
            Signature:  "arda.knowledge.search(args: { query: string; limit?: number }): Promise<KnowledgeSearchResult[]>",
            JSDoc:      "...",
            Keywords:   []string{"knowledge", "search", "faq", "doc", "procedure", "runbook"},
            Kind:       "read",
            RequiredPerms: []string{"ai.knowledge.read"},
            Risk:       "low",
            TimeoutMs:  3000,
        },
    }
}
```

### 3.2 Catalog Generator (CLI tool)

A Go CLI tool `tools/catalog-gen` reads OpenAPI specs and outputs a Go file:

```
$ go run ./tools/catalog-gen \
    --openapi contracts/openapi/crm-v1.json \
    --openapi contracts/openapi/hrm-v1.json \
    --out apps/ai-service/internal/catalog/generated.go
```

The generator:
1. Reads each `x-ai-tool` annotated path/operation.
2. Extracts parameter schema → generates TypeScript parameter type.
3. Extracts `summary` + `description` → generates JSDoc.
4. Emits a Go slice of `CatalogEntry` as a generated file (not runtime reflection).

**Generated file is committed to the repository.** This makes the catalog
auditable (changes show up in git diff) and avoids runtime OpenAPI parsing in
`ai-service`.

### 3.3 Update Workflow

```
Domain team adds x-ai-tool annotation to OpenAPI spec
  │
  ├─ OpenAPI spec merged into contracts/openapi/
  │
  ├─ CI runs: make catalog-gen
  │     reads all annotated specs
  │     regenerates internal/catalog/generated.go
  │
  ├─ PR diff shows new CatalogEntry (reviewable, auditable)
  │
  └─ ai-service deployed with updated catalog
```

There is **no hot-reload or runtime OpenAPI fetch**. The catalog is a static
compile-time artifact. This makes the catalog predictable and eliminates a class
of runtime failure modes.

---

## 4. Search Index — In-Process BM25

### 4.1 Why BM25 (not vector embedding)

- The catalog is small (< 200 entries at full scale). BM25 is O(n) and fast enough.
- BM25 does not require an embedding model, an additional inference call, or a
  vector column. It is purely algorithmic.
- BM25 handles short keyword queries ("crm customer risk") well. Semantic search
  would help for paraphrase queries, but LLMs are already good at translating
  intent to domain keywords.
- Embeddings can be added as a re-ranking layer later without changing the API.

### 4.2 Index Structure

At `ai-service` startup, all `CatalogEntry` records are loaded and a BM25 index
is built over the `Keywords` + `SDKPath` + first sentence of `JSDoc`:

```go
// internal/catalog/index.go
type CatalogIndex struct {
    entries []CatalogEntry
    bm25    *bm25.Index  // thin pure-Go BM25 implementation
}

func NewCatalogIndex(entries []CatalogEntry) *CatalogIndex {
    idx := bm25.New()
    for i, e := range entries {
        doc := strings.Join(append(e.Keywords, e.SDKPath), " ") + " " + firstSentence(e.JSDoc)
        idx.AddDocument(i, doc)
    }
    idx.Build()
    return &CatalogIndex{entries: entries, bm25: idx}
}

func (ci *CatalogIndex) Search(query, domain string, scope tools.Context) []CatalogEntry {
    // 1. Tokenize query
    tokens := tokenize(query)

    // 2. BM25 score all entries
    scores := ci.bm25.Score(tokens)

    // 3. Filter by domain (if specified)
    // 4. Filter by permissions (only include methods the user could call)
    // 5. Return top-5 by score, formatted as TypeScript signatures

    results := []CatalogEntry{}
    for _, hit := range sortByScore(scores) {
        entry := ci.entries[hit.DocID]
        if domain != "" && domain != "all" && entry.Domain != domain {
            continue
        }
        if !hasRequiredPerms(scope.Permissions, entry.RequiredPerms) {
            continue  // silently omit — don't reveal forbidden methods exist
        }
        results = append(results, entry)
        if len(results) >= 5 { break }
    }
    return results
}
```

### 4.3 Permission-Filtered Results

A user without `crm.customer.read` will **not see** `arda.crm.getCustomer` in
`search` results. The catalog is filtered per-request based on `scope.Permissions`.
This prevents the model from suggesting tools the user cannot use, and avoids
leaking the existence of restricted capabilities.

---

## 5. SDK Method Dispatcher Registration

Each `CatalogEntry` is paired with a **Go dispatcher function** that the Goja
sandbox calls when the script invokes the corresponding `arda.*` method.

```go
// internal/catalog/dispatcher.go
type DispatcherFunc func(ctx context.Context, scope tools.Context, args map[string]any) (any, error)

type DispatcherRegistry struct {
    dispatchers map[string]DispatcherFunc
}

func (r *DispatcherRegistry) Register(methodName string, fn DispatcherFunc) {
    r.dispatchers[methodName] = fn
}

func (r *DispatcherRegistry) Resolve(methodName string) (DispatcherFunc, bool) {
    fn, ok := r.dispatchers[methodName]
    return fn, ok
}
```

Dispatchers are registered at startup alongside catalog entries:

```go
// main.go
reg := catalog.NewDispatcherRegistry()
reg.Register("crm.getCustomer", crm.GetCustomerDispatcher(crmClient))
reg.Register("knowledge.search", knowledge.SearchDispatcher(knowledgeSearcher))
```

The Goja sandbox binds the `arda` object by iterating the dispatcher registry
and exposing each method as an async JS function:

```go
// internal/sandbox/bind.go
func bindArda(vm *goja.Runtime, reg *catalog.DispatcherRegistry, scope tools.Context, ctx context.Context) {
    arda := vm.NewObject()
    for _, entry := range reg.All() {
        methodName := entry.MethodName
        dispatcher, _ := reg.Resolve(methodName)
        // Build nested object: arda.crm.getCustomer
        setNestedMethod(arda, entry.SDKPath, func(call goja.FunctionCall) goja.Value {
            return invokeDispatcher(vm, ctx, scope, dispatcher, call)
        })
    }
    _ = vm.Set("arda", arda)
}
```

---

## 6. Catalog Versioning

- The catalog version is a short hash of all entry `MethodName + Signature` strings.
- The version is included in the `info` endpoint response (`/api/copilotkit` with
  `method: "info"`) so clients can detect catalog changes.
- When a method signature changes (e.g., a new required parameter), the method
  version increments and the old signature is kept as a deprecated alias for one
  release cycle.

---

## 7. Catalog Consistency Checks (CI)

The CI pipeline runs the following checks on every PR that touches `contracts/openapi/`:

1. **Schema completeness:** Every `x-ai-tool` annotated endpoint has a valid
   `sdkPath`, `domain`, `kind`, `requiredPerms`, and `timeoutMs`.
2. **Regeneration check:** `make catalog-gen` produces no diff against the
   committed `internal/catalog/generated.go`. If it does, the PR fails with
   "regenerate the SDK catalog: make catalog-gen".
3. **Dispatcher coverage:** Every `CatalogEntry` in `generated.go` has a
   registered dispatcher in `main.go`. Missing dispatchers are caught by a
   startup-time check that panics.
4. **Permission name alignment:** All `requiredPerms` values in the catalog exist
   in the IAM permission registry (verified against a snapshot).
