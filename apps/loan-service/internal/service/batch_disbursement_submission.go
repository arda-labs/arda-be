package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
)

const (
	disbursementWorkflowMaxAttempts = 8
	disbursementWorkflowBatchSize   = 10
)

// Submit validates and reserves a saved batch, then queues its workflow case.
// It does not make a network call to workflow-service, so API latency does not
// depend on Zeebe availability.
func (s *BatchDisbursementService) Submit(ctx context.Context, tenantID, actor, id string, dataVersion int64) (*domain.DisbursementBatch, error) {
	if dataVersion <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "data_version is required")
	}
	batch, err := s.repo.SubmitDisbursementBatch(ctx, tenantID, id, actor, dataVersion)
	if err != nil {
		if err == repository.ErrStaleVersion {
			return nil, ardaerrors.New(ardaerrors.CodeConflict, "batch changed; reload before submitting")
		}
		if err == repository.ErrHeadroomExceeded {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
		}
		return nil, mapRepoError(err)
	}
	return batch, nil
}

// CancelDraft cancels a draft while retaining its rows and audit history.
func (s *BatchDisbursementService) CancelDraft(ctx context.Context, tenantID, actor, id string, dataVersion int64) error {
	if dataVersion <= 0 {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "data_version is required")
	}
	if err := s.repo.CancelDraftDisbursementBatch(ctx, tenantID, id, actor, dataVersion); err != nil {
		if err == repository.ErrStaleVersion {
			return ardaerrors.New(ardaerrors.CodeConflict, "batch changed or is no longer a draft")
		}
		return mapRepoError(err)
	}
	return nil
}

// ProcessWorkflowOutboxOnce claims and processes one bounded batch of work.
// Workflow commands use deterministic batch-derived idempotency keys so an
// expired lease can safely retry after a worker crash.
func (s *BatchDisbursementService) ProcessWorkflowOutboxOnce(ctx context.Context) error {
	items, err := s.repo.ClaimDisbursementWorkflowOutbox(ctx, disbursementWorkflowBatchSize)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.processWorkflowOutboxItem(ctx, item); err != nil {
			slog.Warn("disbursement workflow outbox item failed", "batch_id", item.BatchID, "err", err)
			if failErr := s.repo.FailDisbursementWorkflowOutbox(ctx, item, err.Error(), disbursementWorkflowMaxAttempts); failErr != nil {
				slog.Error("record disbursement workflow outbox failure", "batch_id", item.BatchID, "err", failErr)
			}
		}
	}
	return nil
}

func (s *BatchDisbursementService) processWorkflowOutboxItem(ctx context.Context, item repository.DisbursementWorkflowOutbox) error {
	if s.workflow == nil {
		return fmt.Errorf("workflow client is not configured")
	}
	batch, err := s.Get(ctx, item.TenantID, item.BatchID)
	if err != nil {
		return err
	}
	caseType, titlePrefix, keyPrefix := batchWorkflowSpec(batch.FlowType)
	created, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          item.TenantID,
		CaseType:          caseType,
		Title:             titlePrefix + " — " + batch.ID + " (" + batch.TxnDate + ")",
		PrimaryObjectType: "lnm.disbursement_batch",
		PrimaryObjectID:   batch.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         batch.CreatedBy,
		IdempotencyKey:    batch.ID,
	})
	if err != nil {
		return fmt.Errorf("workflow create case: %w", err)
	}
	if created == nil || created.GetId() == "" {
		return fmt.Errorf("workflow create case returned no case id")
	}
	rows := batchRowVarsList(batch.Rows, nil)
	vars := map[string]any{
		"batchId":               batch.ID,
		"batchType":             batchTypeOf(caseType),
		"caseId":                created.GetId(),
		"caseCode":              created.GetCaseCode(),
		"postingIdempotencyKey": fmt.Sprintf("lnm-disb-batch-%s", batch.ID),
		"disbursementBatch":     disbursementBatchVars(batch, rows),
	}
	if _, err := s.workflow.SubmitCase(ctx, created.GetId(), batch.CreatedBy, vars, fmt.Sprintf("%s-%s-submit", keyPrefix, batch.ID)); err != nil {
		return fmt.Errorf("workflow submit case: %w", err)
	}
	return s.repo.CompleteDisbursementWorkflowOutbox(ctx, item, created.GetId(), created.GetCaseCode())
}

func batchWorkflowSpec(flowType string) (caseType, titlePrefix, keyPrefix string) {
	if flowType == domain.FlowComplete {
		return BatchDisbCompleteCaseType, "Hoàn tất giải ngân theo hồ sơ", "lnm-disb-batch-complete"
	}
	return BatchDisbRegisterCaseType, "Đăng ký giải ngân theo hồ sơ", "lnm-disb-batch-register"
}

// RunWorkflowOutbox keeps the relay alive until the service is shutting down.
func (s *BatchDisbursementService) RunWorkflowOutbox(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.ProcessWorkflowOutboxOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("process disbursement workflow outbox", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
