package domain

import (
	"errors"
	"fmt"
	"math"
)

type PostingStrategy string

const (
	PostingStrategySimple           PostingStrategy = "SIMPLE"
	PostingStrategyBalTypeSplit     PostingStrategy = "BAL_TYPE_SPLIT"
	PostingStrategyDebtGroupReclass PostingStrategy = "DEBT_GROUP_RECLASS"
)

var (
	ErrUnknownPostingStrategy     = errors.New("unknown posting strategy")
	ErrInvalidDebtGroupTransition = errors.New("invalid debt group transition")
	ErrInvalidDebtGroupTransfer   = errors.New("invalid debt group transfer")
)

func ParsePostingStrategy(value string) (PostingStrategy, error) {
	strategy := PostingStrategy(value)
	switch strategy {
	case PostingStrategySimple, PostingStrategyBalTypeSplit, PostingStrategyDebtGroupReclass:
		return strategy, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownPostingStrategy, value)
	}
}

type DebtGroupTransition struct {
	From string
	To   string
}

type DebtGroupTransfer struct {
	From        string
	To          string
	AmountMinor int64
}

type debtGroupTransitionKey struct {
	from string
	to   string
}

// DebtGroupMatrix contains only configured directed transitions. Business
// mappings are loaded from configuration by the eventual 305 integration;
// this task deliberately does not invent any transition data.
type DebtGroupMatrix struct {
	allowed map[debtGroupTransitionKey]struct{}
}

func NewDebtGroupMatrix(transitions []DebtGroupTransition) (*DebtGroupMatrix, error) {
	matrix := &DebtGroupMatrix{allowed: make(map[debtGroupTransitionKey]struct{}, len(transitions))}
	for _, transition := range transitions {
		if transition.From == "" || transition.To == "" || transition.From == transition.To {
			return nil, fmt.Errorf("%w: from and to must be distinct non-empty codes", ErrInvalidDebtGroupTransition)
		}
		key := debtGroupTransitionKey{from: transition.From, to: transition.To}
		if _, exists := matrix.allowed[key]; exists {
			return nil, fmt.Errorf("%w: duplicate %s -> %s", ErrInvalidDebtGroupTransition, transition.From, transition.To)
		}
		matrix.allowed[key] = struct{}{}
	}
	return matrix, nil
}

// Apply returns a new balance map after applying configured transfers. Each
// transfer is conserved exactly in minor units; input balances are unchanged.
func (m *DebtGroupMatrix) Apply(balances map[string]int64, transfers []DebtGroupTransfer) (map[string]int64, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: matrix is nil", ErrInvalidDebtGroupTransfer)
	}
	result := make(map[string]int64, len(balances))
	for group, balance := range balances {
		if group == "" || balance < 0 {
			return nil, fmt.Errorf("%w: group code must be present and balance non-negative", ErrInvalidDebtGroupTransfer)
		}
		result[group] = balance
	}
	for _, transfer := range transfers {
		key := debtGroupTransitionKey{from: transfer.From, to: transfer.To}
		if _, ok := m.allowed[key]; !ok || transfer.AmountMinor <= 0 || transfer.From == transfer.To {
			return nil, fmt.Errorf("%w: %s -> %s amount %d", ErrInvalidDebtGroupTransfer, transfer.From, transfer.To, transfer.AmountMinor)
		}
		if result[transfer.From] < transfer.AmountMinor || result[transfer.To] > math.MaxInt64-transfer.AmountMinor {
			return nil, fmt.Errorf("%w: insufficient source balance or target overflow", ErrInvalidDebtGroupTransfer)
		}
		result[transfer.From] -= transfer.AmountMinor
		result[transfer.To] += transfer.AmountMinor
	}
	return result, nil
}
