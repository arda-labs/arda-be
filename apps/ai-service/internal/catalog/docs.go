package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// docsLookuper fetches problem-catalog pages from the docs site
// (docs.arda.io.vn /api/lookup). Narrow interface so tests can stub it.
type docsLookuper interface {
	Lookup(ctx context.Context, code string) (any, error)
}

// HTTPDocsLookuper queries the public problem-docs lookup API. The docs site
// is public and versioned by the problem code, so no auth headers are sent.
type HTTPDocsLookuper struct {
	baseURL string
	client  *http.Client
}

func NewHTTPDocsLookuper(baseURL string) *HTTPDocsLookuper {
	return &HTTPDocsLookuper{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (l *HTTPDocsLookuper) Lookup(ctx context.Context, code string) (any, error) {
	if l.baseURL == "" {
		return nil, fmt.Errorf("problem docs URL is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.baseURL+"/api/lookup?code="+code, nil)
	if err != nil {
		return nil, fmt.Errorf("docs lookup request error: %w", err)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docs lookup error: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("docs lookup read error: %w", err)
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("docs lookup response error: %w", err)
	}
	return payload, nil
}

// RegisterDocsCatalog registers the arda.docs.* SDK methods — deterministic
// lookup of the problem-details catalog that backs the `type` URLs returned
// by every Arda problem+json error response.
func RegisterDocsCatalog(reg *DispatcherRegistry, docs docsLookuper) {
	if docs == nil {
		return
	}

	// arda.docs.problemLookup (Read)
	reg.Register(
		CatalogEntry{
			MethodName: "docs.problemLookup",
			SDKPath:    "arda.docs.problemLookup",
			Domain:     "docs",
			Signature:  "arda.docs.problemLookup(args: { code: string }): Promise<{ found: boolean; page?: ProblemDoc; suggestions?: ProblemDoc[] }>;",
			JSDoc: `/**
	 * Look up an Arda API error code in the problem-details catalog
	 * (docs.arda.io.vn). Use when a user asks what an error means, why an API
	 * call failed, or how to recover. Pass the exact problem `+"`code`"+` from an
	 * error response (e.g. "recent_auth_required", "validation.invalid_input").
	 * Unknown codes return suggestions of similar documented codes.
	 * @param args.code Stable problem code (dotted or snake_case machine code)
	 * @returns { found, page: { code, title, status, summary, client_action, operator_action, related_routes, url } }
	 * @domain docs
	 */`,
			Keywords:            []string{"docs", "error", "problem", "lookup", "documentation", "troubleshoot", "help", "lỗi", "mã lỗi", "khắc phục", "giải thích"},
			Kind:                "read",
			RequiredPermissions: nil, // any authenticated actor; the catalog is public
			Risk:                "low",
			Timeout:             5 * time.Second,
		},
		func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
			code, _ := args["code"].(string)
			code = strings.TrimSpace(code)
			if code == "" || len(code) > 128 {
				return nil, fmt.Errorf("%w: code is required (max 128 characters)", tools.ErrInvalidArgument)
			}
			return docs.Lookup(ctx, code)
		},
	)
}
