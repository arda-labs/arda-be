package finance

import (
	"context"
	"fmt"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// PostingRuleLister is the narrow finance API needed to load one accounting card.
type PostingRuleLister interface {
	ListPostingRules(context.Context, string) ([]*financev1.PostingRule, error)
}

// PostingLeg is one caller-side posting leg awaiting rule resolution.
type PostingLeg struct {
	// CardLine is the fin_accounting_rules line_no this leg resolves against.
	CardLine int32
	// Direction is the posting side (DEBIT | CREDIT).
	Direction string
	// AmountMinor must be positive. Zero legs are omitted before card lookup.
	AmountMinor int64
	// Analytics carries the line's org/debt/customer/contract scope.
	Analytics *financev1.Analytics
	// Description is the optional per-line description.
	Description string
}

// FetchPostingRules returns a card or a typed business error. Transport errors
// remain ordinary errors so callers can retry them instead of treating an
// unavailable finance service as a missing configuration row.
func FetchPostingRules(ctx context.Context, client PostingRuleLister, documentType string) ([]*financev1.PostingRule, error) {
	if client == nil {
		return nil, NewPostingError(financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND,
			fmt.Errorf("finance client is not configured for document type %s", documentType))
	}
	rules, err := client.ListPostingRules(ctx, documentType)
	if err != nil {
		return nil, fmt.Errorf("list posting rules for %s: %w", documentType, err)
	}
	if len(rules) == 0 {
		return nil, NewPostingError(financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND,
			fmt.Errorf("RULE_NOT_FOUND: no active posting rules for %s", documentType))
	}
	return rules, nil
}

// BuildPostingLines resolves every non-zero leg from its declared card line.
// There is deliberately no hardcoded classification fallback.
func BuildPostingLines(ctx context.Context, client PostingRuleLister, documentType string, legs []PostingLeg, currencyCode string) ([]*financev1.PostingLine, error) {
	active := false
	for _, leg := range legs {
		if leg.AmountMinor < 0 {
			return nil, fmt.Errorf("posting amount for %s line %d must not be negative", documentType, leg.CardLine)
		}
		active = active || leg.AmountMinor > 0
	}
	if !active {
		return nil, nil
	}
	rules, err := FetchPostingRules(ctx, client, documentType)
	if err != nil {
		return nil, err
	}
	return PostingLinesFromRules(documentType, rules, legs, currencyCode)
}

// PostingLinesFromRules builds numbered PostingLine values from the matching
// active card rows. Missing rows or unresolved account mappings are typed errors.
func PostingLinesFromRules(documentType string, rules []*financev1.PostingRule, legs []PostingLeg, currencyCode string) ([]*financev1.PostingLine, error) {
	byLine := make(map[int32]*financev1.PostingRule, len(rules))
	for _, rule := range rules {
		if rule == nil || rule.GetLineNo() <= 0 {
			return nil, fmt.Errorf("posting card %s contains an invalid line", documentType)
		}
		if _, exists := byLine[rule.GetLineNo()]; exists {
			return nil, fmt.Errorf("posting card %s contains duplicate line %d", documentType, rule.GetLineNo())
		}
		byLine[rule.GetLineNo()] = rule
	}
	lines := make([]*financev1.PostingLine, 0, len(legs))
	for _, leg := range legs {
		if leg.AmountMinor == 0 {
			continue
		}
		rule, ok := byLine[leg.CardLine]
		if !ok {
			return nil, NewPostingError(financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND,
				fmt.Errorf("RULE_NOT_FOUND: %s line %d", documentType, leg.CardLine))
		}
		if rule.GetDirection() != leg.Direction {
			return nil, NewPostingError(financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED,
				fmt.Errorf("ACCOUNT_UNRESOLVED: %s line %d direction is %s, want %s", documentType, leg.CardLine, rule.GetDirection(), leg.Direction))
		}
		line := &financev1.PostingLine{
			LineNo:       int32(len(lines) + 1),
			Direction:    leg.Direction,
			AmountMinor:  leg.AmountMinor,
			CurrencyCode: currencyCode,
			Description:  leg.Description,
		}
		switch rule.GetResolutionType() {
		case "CLASS_MAP":
			if rule.GetAccClassification() == "" {
				return nil, unresolvedRule(documentType, leg.CardLine, "CLASS_MAP has no acc_classification")
			}
			line.Analytics = WithClassification(leg.Analytics, rule.GetAccClassification())
		case "FIXED_CODE":
			if rule.GetAccountRef() == "" {
				return nil, unresolvedRule(documentType, leg.CardLine, "FIXED_CODE has no account_ref")
			}
			line.AccountCode = rule.GetAccountRef()
		default:
			return nil, unresolvedRule(documentType, leg.CardLine, "unsupported resolution_type "+rule.GetResolutionType())
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func unresolvedRule(documentType string, line int32, reason string) error {
	return NewPostingError(financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED,
		fmt.Errorf("ACCOUNT_UNRESOLVED: %s line %d: %s", documentType, line, reason))
}

// WithClassification stamps acc_classification onto analytics, preserving all
// other caller-supplied dimensions.
func WithClassification(analytics *financev1.Analytics, classification string) *financev1.Analytics {
	if analytics == nil {
		return &financev1.Analytics{AccClassification: classification}
	}
	return &financev1.Analytics{
		AccClassification: classification,
		DebtGroupCode:     analytics.GetDebtGroupCode(),
		OrgUnitCode:       analytics.GetOrgUnitCode(),
		FundSourceCode:    analytics.GetFundSourceCode(),
		CustomerCode:      analytics.GetCustomerCode(),
		ContractCode:      analytics.GetContractCode(),
		Dimensions:        cloneDimensions(analytics.GetDimensions()),
	}
}

func cloneDimensions(dimensions map[string]string) map[string]string {
	if len(dimensions) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(dimensions))
	for key, value := range dimensions {
		cloned[key] = value
	}
	return cloned
}
