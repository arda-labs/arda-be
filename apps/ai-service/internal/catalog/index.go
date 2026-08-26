package catalog

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// Index provides in-memory BM25-based keyword search over CatalogEntry items.
type Index struct {
	entries []CatalogEntry
	docLens []float64
	avgLen  float64
	docFreq map[string]int
	tf      []map[string]int
}

func NewIndex(entries []CatalogEntry) *Index {
	n := len(entries)
	idx := &Index{
		entries: entries,
		docLens: make([]float64, n),
		docFreq: make(map[string]int),
		tf:      make([]map[string]int, n),
	}

	totalLen := 0
	for i, entry := range entries {
		// Build searchable document from keywords, SDK path, domain, and JSDoc
		text := strings.Join(entry.Keywords, " ") + " " + entry.SDKPath + " " + entry.Domain + " " + entry.JSDoc
		tokens := tokenize(text)
		idx.docLens[i] = float64(len(tokens))
		totalLen += len(tokens)

		tfMap := make(map[string]int)
		seen := make(map[string]struct{})
		for _, token := range tokens {
			tfMap[token]++
			if _, ok := seen[token]; !ok {
				seen[token] = struct{}{}
				idx.docFreq[token]++
			}
		}
		idx.tf[i] = tfMap
	}

	if n > 0 {
		idx.avgLen = float64(totalLen) / float64(n)
	}

	return idx
}

type searchHit struct {
	entry CatalogEntry
	score float64
}

// Search queries the catalog for matching SDK method signatures.
func (idx *Index) Search(query string, domain string, scope tools.Context, maxResults int) []CatalogEntry {
	if maxResults <= 0 {
		maxResults = 5
	}

	queryTokens := tokenize(query)
	n := float64(len(idx.entries))
	if n == 0 {
		return nil
	}

	const k1 = 1.2
	const b = 0.75

	var hits []searchHit

	for i, entry := range idx.entries {
		// Domain filter
		if domain != "" && domain != "all" && !strings.EqualFold(entry.Domain, domain) {
			continue
		}

		// Permission check: omit methods the actor cannot call
		if err := entry.CheckPermissions(scope); err != nil {
			continue
		}

		// Score BM25
		score := 0.0
		docLen := idx.docLens[i]

		for _, token := range queryTokens {
			freq, ok := idx.tf[i][token]
			if !ok || freq == 0 {
				continue
			}

			df := float64(idx.docFreq[token])
			idf := math.Log(1.0 + (n-df+0.5)/(df+0.5))
			if idf < 0 {
				idf = 0.01
			}

			tfNorm := (float64(freq) * (k1 + 1.0)) / (float64(freq) + k1*(1.0-b+b*(docLen/idx.avgLen)))
			score += idf * tfNorm
		}

		// Direct substring / keyword boost
		lowerQuery := strings.ToLower(query)
		for _, kw := range entry.Keywords {
			if strings.Contains(lowerQuery, strings.ToLower(kw)) {
				score += 2.0
			}
		}
		if strings.Contains(strings.ToLower(entry.SDKPath), lowerQuery) {
			score += 5.0
		}

		if score > 0 || len(queryTokens) == 0 {
			hits = append(hits, searchHit{entry: entry, score: score})
		}
	}

	// Sort descending by score
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].score > hits[j].score
	})

	results := make([]CatalogEntry, 0, maxResults)
	for i := 0; i < len(hits) && i < maxResults; i++ {
		results = append(results, hits[i].entry)
	}

	return results
}

// FormatSignatures formats catalog entries into TypeScript definitions and JSDoc for the LLM.
func FormatSignatures(entries []CatalogEntry) string {
	if len(entries) == 0 {
		return "// No matching arda.* SDK methods found for this query."
	}

	var sb strings.Builder
	for i, e := range entries {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		if e.JSDoc != "" {
			sb.WriteString(e.JSDoc)
			sb.WriteString("\n")
		}
		sb.WriteString(e.Signature)
		if !strings.HasSuffix(e.Signature, ";") {
			sb.WriteString(";")
		}
	}
	return sb.String()
}

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	var tokens []string
	var current strings.Builder

	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
