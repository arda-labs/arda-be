package worker

import (
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// Rule-card line build (iteration 11 wave 2; moved verbatim to
// libs/go/arda-grpc/client/finance in iteration 12 so loan-service
// accrual/provision share it). This file is a thin alias layer — the worker
// call sites (disbursement/collection leg builders) keep their unexported
// names and the exported helpers live in the shared finance client package.

// postingLeg aliases the shared worker-side posting leg awaiting rule
// resolution.
type postingLeg = financeclient.PostingLeg

// fetchPostingRules loads the rule card for a document type. Any failure
// degrades to nil — the built-in fallback classifications take over.
func fetchPostingRules(financeClient *financeclient.Client, documentType string) []*financev1.PostingRule {
	return financeclient.FetchPostingRules(financeClient, documentType)
}

// postingLinesFromRules builds the numbered PostingLine list from legs,
// resolving each leg's classification from its card line (CLASS_MAP stamps
// analytics.acc_classification; FIXED_CODE resolves against the COA). A leg
// whose card row is missing, inactive, or unclassified keeps its fallback.
func postingLinesFromRules(rules []*financev1.PostingRule, legs []postingLeg, currencyCode string) []*financev1.PostingLine {
	return financeclient.PostingLinesFromRules(rules, legs, currencyCode)
}
