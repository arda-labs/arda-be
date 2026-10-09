package worker

import (
	"context"
	"errors"
	"testing"

	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/pb"
)

type collectionLoanStub struct{ collectionLoanClient }

func (collectionLoanStub) GetCollectionPostingDetail(context.Context, string) (*loanv1.CollectionPostingDetail, error) {
	return &loanv1.CollectionPostingDetail{CollectionId: "receipt", CollectionDate: "2026-10-09", PrincipalMinor: 100, CurrencyCode: "VND"}, nil
}
func (collectionLoanStub) CheckCollection(context.Context, string) (bool, string, error) {
	return true, "", nil
}

type collectionFailureFinance struct {
	err        error
	releaseErr error
	released   int
	releasedID string
}

func (f *collectionFailureFinance) Reserve(context.Context, *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	return nil, f.err
}
func (f *collectionFailureFinance) Post(context.Context, *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	return nil, f.err
}
func (f *collectionFailureFinance) Release(_ context.Context, req *financev1.ReleaseRequest) (*financev1.PostingResponse, error) {
	f.released++
	f.releasedID = req.GetJournalEntryId()
	return &financev1.PostingResponse{}, f.releaseErr
}
func (*collectionFailureFinance) ListPostingRules(context.Context, string) ([]*financev1.PostingRule, error) {
	return []*financev1.PostingRule{
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "CASH_SETTLEMENT_ACCOUNT"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_LOAN_PRINCIPAL"},
	}, nil
}

func TestCollectionBusinessFailureHandlerReturnsToMaker(t *testing.T) {
	for _, phase := range []string{"init", "validate", "execute"} {
		for _, code := range []financev1.PostingErrorCode{financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED, financev1.PostingErrorCode_POSTING_ERROR_CODE_INSUFFICIENT_BALANCE} {
			t.Run(phase+"/"+code.String(), func(t *testing.T) {
				f := &collectionFailureFinance{err: &financeclient.PostingError{Code: code, Cause: errors.New("business rejection")}}
				jobs := &recordingJobGateway{}
				w := &CollectionWorkers{loanClient: collectionLoanStub{}, financeClient: f}
				job := entities.Job{ActivatedJob: &pb.ActivatedJob{Key: 42, Retries: 3, Variables: `{"collectionId":"receipt","journalEntryId":"pending-id","tenantId":"tenant-a"}`}}
				switch phase {
				case "init":
					w.init()(jobs, job)
				case "validate":
					w.validate()(jobs, job)
				case "execute":
					w.execute()(jobs, job)
				}
				if f.released != 1 || f.releasedID != "pending-id" || jobs.failed != nil || jobs.validation == nil || jobs.validation.GetErrorCode() != ErrorValidationFailed {
					t.Fatalf("release=%d/%s failed=%v validation=%v", f.released, f.releasedID, jobs.failed, jobs.validation)
				}
			})
		}
	}
}

func TestCollectionFailedReleaseRetainsRetries(t *testing.T) {
	f := &collectionFailureFinance{err: &financeclient.PostingError{Code: financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED, Cause: errors.New("closed")}, releaseErr: errors.New("release unavailable")}
	jobs := &recordingJobGateway{}
	w := &CollectionWorkers{loanClient: collectionLoanStub{}, financeClient: f}
	w.execute()(jobs, entities.Job{ActivatedJob: &pb.ActivatedJob{Key: 42, Retries: 3, Variables: `{"collectionId":"receipt","journalEntryId":"pending-id"}`}})
	if f.released != 1 || jobs.validation != nil || jobs.failed == nil || jobs.failed.GetRetries() != 2 {
		t.Fatalf("release=%d failed=%v validation=%v", f.released, jobs.failed, jobs.validation)
	}
}

type cancellationFailureFinance struct {
	cancellationFinanceClient
	err error
}

func (f cancellationFailureFinance) Reverse(context.Context, *financev1.ReverseRequest) (*financev1.PostingResponse, error) {
	return nil, f.err
}

func TestCancellationBusinessFailureHandlerReturnsToMaker(t *testing.T) {
	jobs := &recordingJobGateway{}
	w := &CancellationWorkers{flow: TxnCancelFlow, financeClient: cancellationFailureFinance{err: &financeclient.PostingError{Code: financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED, Cause: errors.New("closed")}}}
	w.execute()(jobs, entities.Job{ActivatedJob: &pb.ActivatedJob{Key: 43, Retries: 3, Variables: `{"journalEntryId":"original-posted","cancellationRequest":{"referenceEntryNo":"1","reason":"correction","accountingDate":"2026-10-09","idempotencyKey":"cancel-key"}}`}})
	if jobs.failed != nil || jobs.validation == nil || jobs.validation.GetErrorCode() != ErrorValidationFailed {
		t.Fatalf("failed=%v validation=%v", jobs.failed, jobs.validation)
	}
}

func TestCollectionTransientFailureKeepsHold(t *testing.T) {
	f := &collectionFailureFinance{err: errors.New("connection reset")}
	jobs := &recordingJobGateway{}
	w := &CollectionWorkers{loanClient: collectionLoanStub{}, financeClient: f}
	w.execute()(jobs, entities.Job{ActivatedJob: &pb.ActivatedJob{Key: 42, Retries: 3, Variables: `{"collectionId":"receipt","journalEntryId":"pending-id"}`}})
	if f.released != 0 || jobs.validation != nil || jobs.failed == nil || jobs.failed.GetRetries() != 2 {
		t.Fatalf("release=%d failed=%v validation=%v", f.released, jobs.failed, jobs.validation)
	}
}

func TestCollectionBusinessFailureWithoutHold(t *testing.T) {
	f := &collectionFailureFinance{err: &financeclient.PostingError{Code: financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED, Cause: errors.New("closed")}}
	jobs := &recordingJobGateway{}
	w := &CollectionWorkers{loanClient: collectionLoanStub{}, financeClient: f}
	w.init()(jobs, entities.Job{ActivatedJob: &pb.ActivatedJob{Key: 42, Retries: 3, Variables: `{"collectionId":"receipt"}`}})
	if f.released != 0 || jobs.validation == nil || jobs.failed != nil {
		t.Fatalf("release=%d failed=%v validation=%v", f.released, jobs.failed, jobs.validation)
	}
}

func TestCancellationTransientFailureRetainsRetries(t *testing.T) {
	jobs := &recordingJobGateway{}
	w := &CancellationWorkers{flow: TxnCancelFlow, financeClient: cancellationFailureFinance{err: errors.New("connection reset")}}
	w.execute()(jobs, entities.Job{ActivatedJob: &pb.ActivatedJob{Key: 43, Retries: 3, Variables: `{"journalEntryId":"original-posted","cancellationRequest":{"referenceEntryNo":"1","reason":"correction","idempotencyKey":"cancel-key"}}`}})
	if jobs.validation != nil || jobs.failed == nil || jobs.failed.GetRetries() != 2 {
		t.Fatalf("failed=%v validation=%v", jobs.failed, jobs.validation)
	}
}
