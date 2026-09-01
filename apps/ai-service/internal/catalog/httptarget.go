package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// ClientSet carries the per-service transport clients used by generated
// entries. A client with an empty BaseURL means the target service is not
// wired for this deployment and its generated tools are skipped.
type ClientSet struct {
	CRM     *svcclient.CRMClient
	Finance *svcclient.FinanceClient
	IAM     *svcclient.IAMClient
}

func (s ClientSet) client(service string) *svcclient.Client {
	switch service {
	case "crm-service":
		if s.CRM == nil {
			return nil
		}
		return s.CRM.Client
	case "finance-service":
		if s.Finance == nil {
			return nil
		}
		return s.Finance.Client
	case "iam-service":
		if s.IAM == nil {
			return nil
		}
		return s.IAM.Client
	default:
		return nil
	}
}

// RegisterGeneratedCatalog registers every entry from
// GeneratedCatalog() (contracts/ai-internal/*.json via tools/catalog-gen)
// onto a single generic HTTP dispatcher. Tool availability follows the same
// deployment rule as the typed catalogs: a tool whose target service has no
// base URL is not registered at all.
func RegisterGeneratedCatalog(reg *DispatcherRegistry, set ClientSet) {
	for _, gen := range GeneratedCatalog() {
		client := set.client(gen.Service)
		if client == nil || client.BaseURL == "" {
			continue
		}
		registerGeneratedEntry(reg, client, gen)
	}
}

func registerGeneratedEntry(reg *DispatcherRegistry, client *svcclient.Client, gen GeneratedEntry) {
	entry := CatalogEntry{
		MethodName:          strings.TrimPrefix(gen.SDKPath, "arda."),
		SDKPath:             gen.SDKPath,
		Domain:              gen.Domain,
		Signature:           gen.Signature,
		JSDoc:               gen.JSDoc,
		Keywords:            gen.Keywords,
		Kind:                gen.Kind,
		RequiredPermissions: gen.RequiredPermissions,
		Risk:                gen.Risk,
		Timeout:             gen.Timeout,
	}
	genCopy := gen
	reg.Register(entry, func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
		return executeGenerated(ctx, client, genCopy, scope, args)
	})
}

// executeGenerated validates arguments against the generated bindings, builds
// the signed delegated request, executes it, unwraps the envelope and prunes
// the response to the declared allowlist.
func executeGenerated(
	ctx context.Context,
	client *svcclient.Client,
	gen GeneratedEntry,
	scope tools.Context,
	args map[string]any,
) (any, error) {
	pathValues := make(map[string]string)
	query := url.Values{}
	var body map[string]any

	for _, arg := range gen.Args {
		raw, present := args[arg.Name]
		value, valueErr := coerceArg(arg, raw, present)
		if valueErr != nil {
			return nil, valueErr
		}
		if value == "" {
			continue
		}
		switch arg.In {
		case "path":
			pathValues[arg.Param] = value
		case "query":
			query.Set(arg.Param, value)
		case "body":
			if body == nil {
				body = make(map[string]any)
			}
			body[arg.Param] = normalizeBodyValue(arg, value)
		}
	}

	// Scope-sourced query parameters: derived from the verified actor scope,
	// never from tool arguments.
	for _, sq := range gen.ScopeQuery {
		switch sq.Scope {
		case "tenant":
			query.Set(sq.Param, scope.TenantID)
		}
	}

	path := gen.Path
	for name, value := range pathValues {
		path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(value))
	}
	if strings.Contains(path, "{") {
		return nil, fmt.Errorf("%w: missing required path parameter for %s", tools.ErrInvalidArgument, gen.SDKPath)
	}

	method := gen.Method
	var bodyReader *strings.Reader
	if len(body) > 0 {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode %s body: %w", gen.SDKPath, err)
		}
		bodyReader = strings.NewReader(string(encoded))
	}

	fullPath := path
	if encoded := query.Encode(); encoded != "" {
		fullPath = path + "?" + encoded
	}

	req, err := client.NewRequest(ctx, method, fullPath, scopeToMetadata(scope))
	if err != nil {
		return nil, err
	}
	if bodyReader != nil {
		req.Body = io.NopCloser(bodyReader)
		req.ContentLength = int64(bodyReader.Len())
		req.Header.Set("Content-Type", "application/json")
	}

	// Mutations never auto-retry; svcclient.Do already handles that split.
	var envelope json.RawMessage
	if err := client.Do(ctx, req, &envelope); err != nil {
		return nil, err
	}

	payload := envelope
	if gen.Envelope != "" {
		var wrapped map[string]json.RawMessage
		if err := json.Unmarshal(envelope, &wrapped); err != nil {
			return nil, fmt.Errorf("decode %s envelope: %w", gen.SDKPath, err)
		}
		inner, ok := wrapped[gen.Envelope]
		if !ok {
			return nil, fmt.Errorf("%s response missing envelope %q", gen.SDKPath, gen.Envelope)
		}
		payload = inner
	}

	if len(gen.ResponseSchema) > 0 && gen.ResponseSchema != "null" {
		pruned, err := pruneJSON(payload, []byte(gen.ResponseSchema))
		if err != nil {
			return nil, fmt.Errorf("redact %s response: %w", gen.SDKPath, err)
		}
		payload = pruned
	}

	var out any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("decode %s payload: %w", gen.SDKPath, err)
	}
	return out, nil
}

// ptr is referenced by generated.go for numeric bound literals.
func ptr[T any](v T) *T { return &v }

// coerceArg converts an SDK argument into its wire string form, enforcing
// type/enum/bounds. A missing optional argument yields nil (no wire param).
func coerceArg(arg GeneratedArg, raw any, present bool) (string, error) {
	if !present || raw == nil {
		if arg.Required {
			return "", fmt.Errorf("%w: %s is required", tools.ErrInvalidArgument, arg.Name)
		}
		return "", nil
	}
	switch arg.Type {
	case "integer", "number":
		num, ok := raw.(float64)
		if !ok {
			return "", fmt.Errorf("%w: %s must be a number", tools.ErrInvalidArgument, arg.Name)
		}
		if arg.Min != nil && num < *arg.Min {
			return "", fmt.Errorf("%w: %s must be >= %v", tools.ErrInvalidArgument, arg.Name, *arg.Min)
		}
		if arg.Max != nil && num > *arg.Max {
			return "", fmt.Errorf("%w: %s must be <= %v", tools.ErrInvalidArgument, arg.Name, *arg.Max)
		}
		if arg.Type == "integer" && num != float64(int64(num)) {
			return "", fmt.Errorf("%w: %s must be an integer", tools.ErrInvalidArgument, arg.Name)
		}
		if arg.Default != "" {
			// Keep the default as string; the service applies it when absent,
			// but an explicit value is always forwarded.
			_ = arg.Default
		}
		return strconv.FormatFloat(num, 'f', -1, 64), nil
	case "boolean":
		b, ok := raw.(bool)
		if !ok {
			return "", fmt.Errorf("%w: %s must be a boolean", tools.ErrInvalidArgument, arg.Name)
		}
		return strconv.FormatBool(b), nil
	default:
		s, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("%w: %s must be a string", tools.ErrInvalidArgument, arg.Name)
		}
		s = strings.TrimSpace(s)
		if arg.MaxLength > 0 && len(s) > arg.MaxLength {
			return "", fmt.Errorf("%w: %s is too long (max %d characters)", tools.ErrInvalidArgument, arg.Name, arg.MaxLength)
		}
		if s == "" && !arg.Required {
			return "", nil
		}
		if s == "" && arg.Required {
			return "", fmt.Errorf("%w: %s is required", tools.ErrInvalidArgument, arg.Name)
		}
		if len(arg.Enum) > 0 {
			transformed := transformValue(arg.Transform, s)
			if !containsString(arg.Enum, transformed) {
				return "", fmt.Errorf("%w: %s must be one of: %s", tools.ErrInvalidArgument, arg.Name, strings.Join(arg.Enum, ", "))
			}
			return transformed, nil
		}
		return transformValue(arg.Transform, s), nil
	}
}

func transformValue(transform, value string) string {
	switch transform {
	case "upper":
		return strings.ToUpper(value)
	case "lower":
		return strings.ToLower(value)
	default:
		return value
	}
}

func normalizeBodyValue(arg GeneratedArg, value string) any {
	switch arg.Type {
	case "integer", "number":
		if num, err := strconv.ParseFloat(value, 64); err == nil {
			return num
		}
	case "boolean":
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return value
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// pruneJSON recursively removes any object property not declared in the
// allowlist schema. Arrays are pruned per element; non-objects pass through
// (typed handlers remain the primary redaction layer).
func pruneJSON(data []byte, allowlistSchema []byte) (json.RawMessage, error) {
	var node pruneSpec
	if err := json.Unmarshal(allowlistSchema, &node); err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	pruned := pruneValue(value, &node)
	out, err := json.Marshal(pruned)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type pruneSpec struct {
	Type       string                `json:"type"`
	Properties map[string]*pruneSpec `json:"properties"`
	Items      *pruneSpec            `json:"items"`
}

func pruneValue(value any, spec *pruneSpec) any {
	switch typed := value.(type) {
	case map[string]any:
		if len(spec.Properties) == 0 {
			return value
		}
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if childSpec, ok := spec.Properties[key]; ok && childSpec != nil {
				out[key] = pruneValue(child, childSpec)
			}
			// Undeclared keys are dropped (allowlist semantics).
		}
		return out
	case []any:
		if spec.Items == nil {
			return value
		}
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = pruneValue(item, spec.Items)
		}
		return out
	default:
		return value
	}
}
