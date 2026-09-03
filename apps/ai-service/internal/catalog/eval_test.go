package catalog

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// evalQuestion mirrors one entry of testdata/catalog-eval.json.
type evalQuestion struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Question string   `json:"question"`
	Expected []string `json:"expected"`
}

type evalFile struct {
	Version     int            `json:"version"`
	Description string         `json:"description"`
	Questions   []evalQuestion `json:"questions"`
}

func loadEvalQuestions(t *testing.T) []evalQuestion {
	t.Helper()
	raw, err := os.ReadFile("testdata/catalog-eval.json")
	if err != nil {
		t.Fatalf("read eval set: %v", err)
	}
	var set evalFile
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatalf("decode eval set: %v", err)
	}
	if len(set.Questions) == 0 {
		t.Fatal("eval set has no questions")
	}
	return set.Questions
}

// stubSearcher satisfies ragSearcher so RegisterKnowledgeCatalog actually
// registers arda.knowledge.search in the eval index. It returns no hits —
// this suite measures retrieval of the method, not search quality.
type stubSearcher struct{}

func (stubSearcher) Search(ctx context.Context, md metadata.Context, query string, topK int) (*svcclient.RAGResponse, error) {
	return &svcclient.RAGResponse{Hits: nil}, nil
}

// evalRegistry builds the same registry the running service builds, with
// every service wired (worst case: largest catalog the model could see).
func evalRegistry(t *testing.T) (*DispatcherRegistry, *Index) {
	t.Helper()
	reg := NewDispatcherRegistry()
	RegisterBuiltinCatalog(reg, stubSearcher{})
	RegisterGeneratedCatalog(reg, genClients("http://iam.local", "http://crm.local", "http://finance.local"))
	set := genClients("", "", "")
	set.HRM = testHRMClient("http://hrm.local")
	RegisterGeneratedCatalog(reg, set)
	return reg, NewIndex(reg.AllEntries())
}

// evalScope holds every domain permission so retrieval is measured, not
// authorization.
func evalScope() tools.Context {
	scope := iamScope()
	for _, perm := range []string{"iam.user.read", "hrm.read", "crm.customer.manage", "ai.knowledge.read"} {
		scope.Permissions[perm] = struct{}{}
	}
	return scope
}

// TestCatalogEval_Retrieval runs the golden questions through the BM25 index
// exactly as the search() meta-tool does (same tokenizer, same permission
// filter, same top-5 window) and asserts every expected method lands in the
// result. Keyword questions are hard gates — a miss here is a regression.
// Paraphrase questions report the miss rate without failing while the
// catalog is small; they become the WP8 trigger instrument once it grows.
func TestCatalogEval_Retrieval(t *testing.T) {
	questions := loadEvalQuestions(t)
	_, idx := evalRegistry(t)
	scope := evalScope()

	var keywordMiss, paraphraseMiss, boundaryMiss int
	var paraphraseTotal int

	for _, q := range questions {
		hits := idx.Search(q.Question, "", scope, 5)
		got := make(map[string]bool, len(hits))
		for _, hit := range hits {
			got[strings.TrimPrefix(hit.SDKPath, "arda.")] = true
		}
		missed := false
		for _, want := range q.Expected {
			if !got[want] {
				missed = true
				t.Logf("MISS [%s/%s] %q → want %s (top5: %v)", q.Category, q.ID, q.Question, want, sdkPaths(hits))
			}
		}
		switch q.Category {
		case "paraphrase":
			paraphraseTotal++
			if missed {
				paraphraseMiss++
			}
		case "boundary":
			if missed {
				boundaryMiss++
				t.Errorf("boundary miss [%s] %q — discriminative retrieval must hold", q.ID, q.Question)
			}
		default: // keyword
			if missed {
				keywordMiss++
				t.Errorf("keyword miss [%s] %q — index must match its own terms", q.ID, q.Question)
			}
		}
	}

	// Instrument, not gate: log the paraphrase rate so the WP8 trigger is a
	// number in CI output, not a guess. Flip this log into a hard failure
	// once the catalog is large enough that semantic re-ranking (WP8) is
	// warranted — the runbook threshold is 20%.
	rate := 0.0
	if paraphraseTotal > 0 {
		rate = float64(paraphraseMiss) / float64(paraphraseTotal)
	}
	t.Logf("catalog eval: %d questions | keyword %d/%d miss | boundary %d miss | paraphrase %.0f%% miss (%d/%d)",
		len(questions), keywordMiss, countCategory(questions, "keyword"), boundaryMiss, rate*100, paraphraseMiss, paraphraseTotal)
	if rate > 0.20 {
		t.Logf("WARNING: paraphrase miss rate %.0f%% exceeds the 20%% WP8 trigger threshold", rate*100)
	}
}

func countCategory(questions []evalQuestion, category string) int {
	n := 0
	for _, q := range questions {
		if q.Category == category {
			n++
		}
	}
	return n
}

func sdkPaths(entries []CatalogEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.SDKPath)
	}
	return paths
}
