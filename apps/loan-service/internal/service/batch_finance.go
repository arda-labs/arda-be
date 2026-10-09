package service

import (
	"context"
	"log/slog"

	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

type batchPostingFinance interface {
	Post(context.Context, *financev1.PostingRequest) (*financev1.PostingResponse, error)
	ListPostingRules(context.Context, string) ([]*financev1.PostingRule, error)
}

func batchPostingRules(ctx context.Context, client batchPostingFinance, documentType string) []*financev1.PostingRule {
	if client == nil {
		return nil
	}
	rules, err := client.ListPostingRules(ctx, documentType)
	if err != nil {
		slog.Warn("posting rule lookup failed — falling back to built-in legs", "documentType", documentType, "err", err)
		return nil
	}
	return rules
}

var _ batchPostingFinance = (*financeclient.Client)(nil)
