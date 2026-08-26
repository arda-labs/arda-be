# Multi-Provider & Model Routing Design

Status: **Design specification — implement after Code Mode Phase 2 (Gate 7)**.
The single-provider config exists today; this document defines how to extend
it to support multiple model providers and per-tenant/per-route routing.

---

## 1. Current State

`ai-service` reads a single provider from environment variables:

```env
AI_PROVIDER=openai           # or "anthropic" | "gemini" | "ollama"
AI_MODEL_ID=gpt-4o-mini
AI_API_KEY=sk-...
```

The `model.Provider` interface (`internal/model/provider.go`) already abstracts
the provider, so the routing layer is a thin addition on top.

---

## 2. Routing Dimensions

Provider selection must support four dimensions:

| Dimension | Example |
|:---|:---|
| **Tenant plan** | Enterprise tenants → GPT-4o; Starter → GPT-4o-mini |
| **Request risk** | High-risk mutation planning → GPT-4o; Knowledge lookup → local |
| **Feature flag** | Code Mode → reasoning model; direct tools → fast model |
| **Fallback** | Primary unavailable → switch to secondary |

---

## 3. Provider Registry

```go
// internal/model/registry.go
type ProviderID string

const (
    ProviderOpenAI    ProviderID = "openai"
    ProviderAnthropic ProviderID = "anthropic"
    ProviderGemini    ProviderID = "gemini"
    ProviderOllama    ProviderID = "ollama"
)

type ProviderConfig struct {
    ID          ProviderID
    ModelID     string
    BaseURL     string         // for Ollama or custom endpoints
    APIKeyEnv   string         // env var name, not the key itself
    MaxTokens   int
    TimeoutMs   int
    Priority    int            // lower = higher priority in fallback chain
}

type ProviderRegistry struct {
    providers map[ProviderID]Provider
    configs   []ProviderConfig  // ordered by Priority
}

func (r *ProviderRegistry) Select(ctx RoutingContext) Provider {
    for _, cfg := range r.configs {
        if r.matchesContext(cfg, ctx) && r.isHealthy(cfg.ID) {
            return r.providers[cfg.ID]
        }
    }
    return r.providers[r.configs[len(r.configs)-1].ID] // last-resort fallback
}
```

### Routing Context

```go
type RoutingContext struct {
    TenantPlan   string  // "starter" | "professional" | "enterprise"
    RiskLevel    string  // "low" | "medium" | "high"
    FeatureFlags []string
    RunID        string  // for trace correlation
}
```

---

## 4. Config File (not env vars)

Move from env vars to a YAML config mounted as a Kubernetes ConfigMap:

```yaml
# ai-service/config/providers.yaml
providers:
  - id: openai-gpt4o
    provider: openai
    model_id: gpt-4o
    api_key_env: OPENAI_API_KEY
    max_tokens: 4096
    timeout_ms: 30000
    priority: 1
    match:
      tenant_plan: ["enterprise"]

  - id: openai-gpt4o-mini
    provider: openai
    model_id: gpt-4o-mini
    api_key_env: OPENAI_API_KEY
    max_tokens: 2048
    timeout_ms: 20000
    priority: 2
    match:
      tenant_plan: ["starter", "professional"]

  - id: ollama-local
    provider: ollama
    model_id: llama3.1:8b
    base_url: http://ollama.internal:11434
    max_tokens: 2048
    timeout_ms: 60000
    priority: 10
    match:
      feature_flags: ["local_model"]
```

### Security: API keys are never in the config file.

`api_key_env` names an environment variable; the Go process reads the key at
startup from the environment (injected via Kubernetes Secret). The ConfigMap
is safe to commit to the repo.

---

## 5. Health Check & Circuit Breaker

Each provider has an independent circuit breaker:

```
CLOSED → (5 consecutive failures) → OPEN
OPEN   → (60s timeout) → HALF-OPEN → (1 success) → CLOSED
```

Health is checked:
- **Passively:** errors from live requests increment the failure counter.
- **Actively:** a lightweight ping (empty completion with 1-token limit) runs
  every 30 seconds for OPEN providers to detect recovery.

When a provider is OPEN, `Select()` skips it and tries the next in priority
order. If all providers are OPEN, the request fails with `ai.model_unavailable`.

---

## 6. Tenant Plan Resolution

Provider selection based on tenant plan requires reading plan metadata. The
resolver queries the IAM service (or a cached JWT claim):

```go
// RoutingContext is populated from tools.Context before model call
func buildRoutingContext(ctx tools.Context, features []string) RoutingContext {
    return RoutingContext{
        TenantPlan:   ctx.TenantPlan,    // from JWT claim or IAM cache
        RiskLevel:    ctx.RiskLevel,
        FeatureFlags: features,
        RunID:        ctx.RunID,
    }
}
```

**Cache:** Tenant plan is cached per tenant ID for 5 minutes to avoid IAM
round-trips on every request. Cache invalidation occurs on tenant plan change
events (NATS).

---

## 7. Audit & Cost Attribution

Every run record in `ai_runs` includes:

- `provider`: the provider ID used (e.g. `"openai-gpt4o-mini"`)
- `model_id`: the specific model ID
- `input_tokens`, `output_tokens`: usage from the provider response
- `provider_latency_ms`: time spent in the provider call

These fields enable per-tenant, per-provider cost attribution in the operational
dashboard without requiring a separate billing service.

---

## 8. Rollout Plan

1. **Phase 1 (current):** Single provider via env vars. No change required.
2. **Phase 2 (after Code Mode stable):** Introduce `providers.yaml` config with
   a single provider entry. Validate the config-driven path works identically.
3. **Phase 3:** Add a second provider (e.g., Ollama for local testing). Enable
   circuit breaker. Test fallback behavior.
4. **Phase 4:** Enable tenant-plan-based routing for enterprise vs. starter.
5. **Phase 5:** Enable feature-flag-based routing for Code Mode reasoning model.

Do not skip phases. Each phase must be validated in staging before proceeding.
