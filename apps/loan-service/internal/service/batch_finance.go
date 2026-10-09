package service

import (
	"context"

	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

type batchPostingFinance interface {
	financeclient.PostingRuleLister
	Post(context.Context, *financev1.PostingRequest) (*financev1.PostingResponse, error)
}

var _ batchPostingFinance = (*financeclient.Client)(nil)
