package model

import "strings"

// Pricing is expressed in USD per one million tokens. Unknown models return
// zero rather than inventing a price; operators can still observe token usage.
type Pricing struct {
	PromptPerMillion     float64
	CompletionPerMillion float64
}

var pricingCatalog = map[string]Pricing{
	"gpt-4o-mini":       {PromptPerMillion: 0.15, CompletionPerMillion: 0.60},
	"gpt-4.1-mini":      {PromptPerMillion: 0.40, CompletionPerMillion: 1.60},
	"gemini-2.5-flash":  {PromptPerMillion: 0.30, CompletionPerMillion: 2.50},
	"claude-3-5-sonnet": {PromptPerMillion: 3.00, CompletionPerMillion: 15.00},
}

func EstimateCost(modelID string, promptTokens, completionTokens int) float64 {
	key := strings.ToLower(strings.TrimSpace(modelID))
	price, ok := pricingCatalog[key]
	if !ok {
		return 0
	}
	return float64(promptTokens)/1_000_000*price.PromptPerMillion +
		float64(completionTokens)/1_000_000*price.CompletionPerMillion
}
