package service

import (
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

func TestPostingStrategyProtoMapping(t *testing.T) {
	tests := []struct {
		value string
		want  financev1.PostingStrategy
	}{
		{"SIMPLE", financev1.PostingStrategy_POSTING_STRATEGY_SIMPLE},
		{"BAL_TYPE_SPLIT", financev1.PostingStrategy_POSTING_STRATEGY_BAL_TYPE_SPLIT},
		{"DEBT_GROUP_RECLASS", financev1.PostingStrategy_POSTING_STRATEGY_DEBT_GROUP_RECLASS},
	}
	for _, tt := range tests {
		got, err := postingStrategyProto(tt.value)
		if err != nil || got != tt.want {
			t.Errorf("postingStrategyProto(%q) = %v, %v; want %v", tt.value, got, err, tt.want)
		}
	}
	if got, err := postingStrategyProto("unexpected"); err == nil || got != financev1.PostingStrategy_POSTING_STRATEGY_UNSPECIFIED {
		t.Fatalf("unknown strategy = %v, %v; want unspecified and error", got, err)
	}
}
