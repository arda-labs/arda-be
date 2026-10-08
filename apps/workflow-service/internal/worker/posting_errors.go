package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// classifyPostingError treats only explicit business/policy codes as
// maker-fixable. Unknown status/errors stay transient and retain job retries.
func classifyPostingError(err error) (financev1.PostingErrorCode, bool) {
	if err == nil {
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_UNSPECIFIED, false
	}
	if typed, ok := financeclient.AsPostingError(err); ok {
		return typed.Code, isBusinessPostingErrorCode(typed.Code)
	}
	message := strings.ToUpper(err.Error())
	for name, value := range financev1.PostingErrorCode_value {
		const prefix = "POSTING_ERROR_CODE_"
		if !strings.HasPrefix(name, prefix) || name == prefix+"UNSPECIFIED" {
			continue
		}
		code := strings.TrimPrefix(name, prefix)
		if strings.Contains(message, code) {
			parsed := financev1.PostingErrorCode(value)
			return parsed, isBusinessPostingErrorCode(parsed)
		}
	}
	// Older finance servers predate ErrorInfo and emit these four stable
	// posting-date sentinels in the status message.
	legacyPolicyCodes := []struct {
		fragment string
		code     financev1.PostingErrorCode
	}{
		{"BACKDATE_NOT_ALLOWED", financev1.PostingErrorCode_POSTING_ERROR_CODE_BACKDATE_NOT_ALLOWED},
		{"TRANSACTION_DATE_EXCEEDS_CURRENT_DATE", financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_CURRENT_DATE},
		{"TRANSACTION_DATE_EXCEEDS_BACKDATE", financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_BACKDATE},
		{"POSTING_DATE_BEFORE_CLOSING_LOCK", financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_BEFORE_CLOSING_LOCK},
	}
	for _, item := range legacyPolicyCodes {
		if strings.Contains(message, item.fragment) {
			return item.code, true
		}
	}
	return financev1.PostingErrorCode_POSTING_ERROR_CODE_UNSPECIFIED, false
}

func isBusinessPostingErrorCode(code financev1.PostingErrorCode) bool {
	switch code {
	case financev1.PostingErrorCode_POSTING_ERROR_CODE_UNBALANCED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_BACKDATE_NOT_ALLOWED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_INSUFFICIENT_BALANCE,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_DUPLICATE_IDEMPOTENCY_KEY_CONFLICT,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_NOT_FOUND,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_INVALID_AMOUNT,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_CURRENCY_MISMATCH,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_CURRENT_DATE,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_BACKDATE,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_BEFORE_CLOSING_LOCK:
		return true
	default:
		return false
	}
}

// routePostingFailure releases any existing PENDING hold before returning a
// business failure to the maker. A failed release remains transient so the
// job retries until the hold is safely gone.
func routePostingFailure(err error, journalEntryID string, release func() error,
	onBusiness func(financev1.PostingErrorCode, string), onTransient func(error)) {
	code, business := classifyPostingError(err)
	if !business {
		onTransient(err)
		return
	}
	if journalEntryID != "" && release != nil {
		if releaseErr := release(); releaseErr != nil {
			onTransient(fmt.Errorf("posting failed with %s; releasing hold failed: %w", code.String(), releaseErr))
			return
		}
	}
	onBusiness(code, err.Error())
}

func throwPostingValidationError(ctx context.Context, client worker.JobClient, job entities.Job,
	projection *CaseProjection, code financev1.PostingErrorCode, message string) {
	codeName := strings.TrimPrefix(code.String(), "POSTING_ERROR_CODE_")
	i18nKey := "finance.posting.errors." + strings.ToLower(codeName)
	params := map[string]string{"code": codeName, "message": message}
	if projection != nil && projection.caseRepo != nil {
		vars, _ := job.GetVariablesAsMap()
		caseID := stringVariable(vars, "caseId", "case_id")
		if caseID != "" {
			note, _ := json.Marshal(map[string]any{"i18nKey": i18nKey, "params": params})
			if err := projection.caseRepo.AddTimelineEventInternal(ctx, caseID, "POSTING_VALIDATION_FAILED", string(note)); err != nil {
				// The durable Zeebe boundary error still returns to the maker even
				// if this secondary projection cannot be written.
				logWorkerError("posting validation timeline write failed", job, err)
			}
		}
	}
	throwValidationError(client, job, i18nKey+" ("+codeName+")")
}

func logWorkerError(message string, job entities.Job, err error) {
	slog.Error(message, "jobKey", job.GetKey(), "jobType", job.GetType(), "err", err)
}
