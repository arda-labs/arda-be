package worker

import (
	"context"
	"log/slog"

	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// Rule-card line build (iteration 11 wave 2). Domain flows used to hardcode
// their classification strings per leg; they now fetch the seeded
// fin_accounting_rules card via finance ListPostingRules and only fall back
// to the built-in classifications when the card has no row for the leg — a
// rule lookup failure must never break a flow that was already working.

// postingLeg is one worker-side posting leg awaiting rule resolution.
type postingLeg struct {
	// CardLine is the fin_accounting_rules line_no this leg resolves its
	// classification from. Explicit (not positional) so conditional legs —
	// collection posts its interest pair only when interest > 0 — keep
	// pointing at their own card rows.
	CardLine int32
	// Fallback is the pre-rules hardcoded classification, used when the
	// card has no usable row for this leg.
	Fallback string
	// Direction is the posting side (DEBIT | CREDIT) for the built line.
	Direction string
	// AmountMinor must be > 0; callers skip zero legs before building.
	AmountMinor int64
	// Analytics carries the line's org/debt/customer/contract scope; the
	// classification is stamped by postingLinesFromRules.
	Analytics *financev1.Analytics
	// Description is the optional per-line description.
	Description string
}

// fetchPostingRules loads the rule card for a document type. Any failure
// degrades to nil — the built-in fallback classifications take over.
func fetchPostingRules(financeClient *financeclient.Client, documentType string) []*financev1.PostingRule {
	if financeClient == nil {
		return nil
	}
	rules, err := financeClient.ListPostingRules(context.Background(), documentType)
	if err != nil {
		slog.Warn("posting rule lookup failed — falling back to built-in legs", "documentType", documentType, "err", err)
		return nil
	}
	return rules
}

// postingLinesFromRules builds the numbered PostingLine list from legs,
// resolving each leg's classification from its card line. CLASS_MAP rows
// stamp analytics.acc_classification; FIXED_CODE rows resolve the line
// directly against the COA (account_code). A leg whose card row is missing,
// inactive, or unclassified keeps its fallback classification with a warn.
func postingLinesFromRules(rules []*financev1.PostingRule, legs []postingLeg, currencyCode string) []*financev1.PostingLine {
	byLine := make(map[int32]*financev1.PostingRule, len(rules))
	for _, rule := range rules {
		byLine[rule.GetLineNo()] = rule
	}
	lines := make([]*financev1.PostingLine, 0, len(legs))
	for _, leg := range legs {
		if leg.AmountMinor <= 0 {
			continue
		}
		classification := leg.Fallback
		warnReason := ""
		rule, ok := byLine[leg.CardLine]
		switch {
		case !ok:
			warnReason = "rule row missing"
		case rule.GetResolutionType() == "FIXED_CODE" && rule.GetAccountRef() == "":
			warnReason = "FIXED_CODE rule without account_ref"
		case rule.GetResolutionType() != "FIXED_CODE" && rule.GetAccClassification() == "":
			warnReason = "CLASS_MAP rule without acc_classification"
		}
		if warnReason == "" {
			if rule.GetResolutionType() == "FIXED_CODE" {
				classification = ""
			} else {
				classification = rule.GetAccClassification()
			}
		} else {
			slog.Warn("posting rule card row unusable — using built-in classification",
				"cardLine", leg.CardLine, "reason", warnReason, "fallback", leg.Fallback)
		}
		line := &financev1.PostingLine{
			LineNo:       int32(len(lines) + 1),
			Direction:    leg.Direction,
			AmountMinor:  leg.AmountMinor,
			CurrencyCode: currencyCode,
			Description:  leg.Description,
		}
		if rule != nil && warnReason == "" && rule.GetResolutionType() == "FIXED_CODE" {
			line.AccountCode = rule.GetAccountRef()
		} else {
			line.Analytics = withClassification(leg.Analytics, classification)
		}
		lines = append(lines, line)
	}
	return lines
}

// withClassification stamps acc_classification onto the analytics (or creates
// one), preserving whatever the caller already set.
func withClassification(analytics *financev1.Analytics, classification string) *financev1.Analytics {
	if analytics == nil {
		return &financev1.Analytics{AccClassification: classification}
	}
	analytics.AccClassification = classification
	return analytics
}
