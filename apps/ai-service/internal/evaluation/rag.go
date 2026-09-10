package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"gopkg.in/yaml.v3"
)

type Set struct {
	Version     int      `yaml:"version"`
	Description string   `yaml:"description"`
	Metrics     []string `yaml:"metrics"`
	Cases       []Case   `yaml:"cases"`
}
type Case struct {
	ID       string   `yaml:"id"`
	Tenant   string   `yaml:"tenant"`
	Query    string   `yaml:"query"`
	Expected Expected `yaml:"expected"`
}
type Expected struct {
	AnswerMustCite       bool     `yaml:"answer_must_cite"`
	SourceKeys           []string `yaml:"source_keys"`
	AllowNoAnswer        bool     `yaml:"allow_no_answer"`
	ExcludeExpired       bool     `yaml:"exclude_expired"`
	MustNotReturnTenants []string `yaml:"must_not_return_tenants"`
}

func Parse(data []byte) (Set, error) {
	var set Set
	if err := yaml.Unmarshal(data, &set); err != nil {
		return Set{}, fmt.Errorf("decode evaluation set: %w", err)
	}
	if len(set.Cases) == 0 {
		return Set{}, fmt.Errorf("evaluation set has no cases")
	}
	return set, nil
}

type QueryFunc func(context.Context, Case) (knowledge.QueryResponse, error)
type CaseResult struct {
	ID                string  `json:"id"`
	Hits              int     `json:"hits"`
	Recall            float64 `json:"recall"`
	CitationAvailable bool    `json:"citation_available"`
	Passed            bool    `json:"passed"`
	Error             string  `json:"error,omitempty"`
	LatencyMs         int     `json:"latency_ms"`
}
type Report struct {
	Version          int          `json:"version"`
	Cases            []CaseResult `json:"cases"`
	Passed           int          `json:"passed"`
	Failed           int          `json:"failed"`
	RecallAtK        float64      `json:"recall_at_k"`
	CitationCoverage float64      `json:"citation_coverage"`
	DurationMs       int64        `json:"duration_ms"`
}

func Run(ctx context.Context, set Set, query QueryFunc) Report {
	started := time.Now()
	report := Report{Version: set.Version, Cases: make([]CaseResult, 0, len(set.Cases))}
	var recallSum float64
	for _, c := range set.Cases {
		result := CaseResult{ID: c.ID}
		res, err := query(ctx, c)
		if err != nil {
			result.Error = err.Error()
			report.Failed++
			report.Cases = append(report.Cases, result)
			continue
		}
		result.Hits, result.LatencyMs = len(res.Hits), res.LatencyMs
		found := map[string]bool{}
		for _, hit := range res.Hits {
			found[strings.ToLower(hit.SourceKey)] = true
			if hit.Citation != "" || hit.CitationRef.Locator != "" {
				result.CitationAvailable = true
			}
		}
		matched := 0
		for _, key := range c.Expected.SourceKeys {
			if found[strings.ToLower(key)] {
				matched++
			}
		}
		if len(c.Expected.SourceKeys) > 0 {
			result.Recall = float64(matched) / float64(len(c.Expected.SourceKeys))
			recallSum += result.Recall
		}
		if c.Expected.AllowNoAnswer {
			result.Passed = len(res.Hits) == 0
		} else if c.Expected.AnswerMustCite {
			result.Passed = result.CitationAvailable && matched == len(c.Expected.SourceKeys)
		} else {
			result.Passed = matched == len(c.Expected.SourceKeys)
		}
		if len(c.Expected.SourceKeys) == 0 && !c.Expected.AllowNoAnswer && !result.Passed {
			result.Passed = result.CitationAvailable
		}
		if result.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
		if result.CitationAvailable {
			report.CitationCoverage++
		}
		report.Cases = append(report.Cases, result)
	}
	if len(set.Cases) > 0 {
		report.RecallAtK = recallSum / float64(len(set.Cases))
		report.CitationCoverage /= float64(len(set.Cases))
	}
	report.DurationMs = time.Since(started).Milliseconds()
	return report
}

// HTTPQuery returns a QueryFunc for the service/gateway RAG endpoint. Identity
// is supplied by the caller so the runner can exercise tenant isolation. When
// cookie is non-empty (or AI_EVAL_COOKIE is set), it is sent as the Cookie header.
// When AI_EVAL_SERVICE_SECRET is set, every request is signed with the workload
// identity expected by ai-service in production (source auth-gateway).
func HTTPQuery(baseURL, userID, tenantID, permissions, cookie string, client *http.Client) QueryFunc {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	serviceSecret := strings.TrimSpace(os.Getenv("AI_EVAL_SERVICE_SECRET"))
	return func(ctx context.Context, c Case) (knowledge.QueryResponse, error) {
		body, _ := json.Marshal(knowledge.QueryRequest{Query: c.Query, TopK: 10})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/rag/query", strings.NewReader(string(body)))
		if err != nil {
			return knowledge.QueryResponse{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Auth-Checked", "true")
		req.Header.Set("X-User-Id", userID)
		caseTenant := c.Tenant
		if caseTenant == "" {
			caseTenant = tenantID
		}
		req.Header.Set("X-Tenant-Id", caseTenant)
		req.Header.Set("X-Permissions", permissions)
		cookieHeader := strings.TrimSpace(cookie)
		if cookieHeader == "" {
			cookieHeader = strings.TrimSpace(os.Getenv("AI_EVAL_COOKIE"))
		}
		if cookieHeader != "" {
			req.Header.Set("Cookie", cookieHeader)
		}
		if serviceSecret != "" {
			if err := identity.SignRequest(req, serviceSecret, "auth-gateway", "ai-service", time.Now(), 2*time.Minute); err != nil {
				return knowledge.QueryResponse{}, err
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return knowledge.QueryResponse{}, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if resp.StatusCode/100 != 2 {
			return knowledge.QueryResponse{}, fmt.Errorf("rag query returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}
		var out knowledge.QueryResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return out, err
		}
		return out, nil
	}
}
