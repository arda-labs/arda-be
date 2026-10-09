package worker

import (
	"context"
	"fmt"

	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// Rule-card line build (iteration 11 wave 2; moved verbatim to
// libs/go/arda-grpc/client/finance in iteration 12 so loan-service
// accrual/provision share it). This file is a thin alias layer — the worker
// call sites (disbursement/collection leg builders) keep their unexported
// names and the exported helpers live in the shared finance client package.

// postingLeg aliases the shared worker-side posting leg awaiting rule
// resolution.
type postingLeg = financeclient.PostingLeg

// postingLinesFromRules requires the finance rule card and returns typed
// RULE_NOT_FOUND / ACCOUNT_UNRESOLVED errors. There is no built-in fallback.
func postingLinesFromRules(ctx context.Context, client financeclient.PostingRuleLister, documentType string, legs []postingLeg, currencyCode string) ([]*financev1.PostingLine, error) {
	return financeclient.BuildPostingLines(ctx, client, documentType, legs, currencyCode)
}

func handlePostingBuildFailure(ctx context.Context, client worker.JobClient, job entities.Job, projection *CaseProjection, err error, retry func(error)) {
	if err == nil {
		return
	}
	code, business := classifyPostingError(err)
	if business {
		throwPostingValidationError(ctx, client, job, projection, code, err.Error())
		return
	}
	retry(fmt.Errorf("build posting request: %w", err))
}
