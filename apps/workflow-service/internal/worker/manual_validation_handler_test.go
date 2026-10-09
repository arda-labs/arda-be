package worker

import (
	"context"
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/commands"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/pb"
	"google.golang.org/grpc"
)

type invalidFinanceStub struct {
	manualPostingFinance
	message  string
	released int
}

func (f *invalidFinanceStub) Validate(context.Context, *financev1.PostingRequest) (*financev1.ValidationResult, error) {
	return &financev1.ValidationResult{Valid: false, GlobalErrors: []string{f.message}}, nil
}

func (f *invalidFinanceStub) Release(context.Context, *financev1.ReleaseRequest) (*financev1.PostingResponse, error) {
	f.released++
	return &financev1.PostingResponse{}, nil
}

type recordingJobGateway struct {
	pb.GatewayClient
	validation *pb.ThrowErrorRequest
	failed     *pb.FailJobRequest
}

func (g *recordingJobGateway) ThrowError(_ context.Context, req *pb.ThrowErrorRequest, _ ...grpc.CallOption) (*pb.ThrowErrorResponse, error) {
	g.validation = req
	return &pb.ThrowErrorResponse{}, nil
}

func (g *recordingJobGateway) FailJob(_ context.Context, req *pb.FailJobRequest, _ ...grpc.CallOption) (*pb.FailJobResponse, error) {
	g.failed = req
	return &pb.FailJobResponse{}, nil
}

func (g *recordingJobGateway) NewCompleteJobCommand() commands.CompleteJobCommandStep1 {
	return commands.NewCompleteJobCommand(g, func(context.Context, error) bool { return false })
}
func (g *recordingJobGateway) NewFailJobCommand() commands.FailJobCommandStep1 {
	return commands.NewFailJobCommand(g, func(context.Context, error) bool { return false })
}
func (g *recordingJobGateway) NewThrowErrorCommand() commands.ThrowErrorCommandStep1 {
	return commands.NewThrowErrorCommand(g, func(context.Context, error) bool { return false })
}

func TestManualValidateInvalidResultHandler(t *testing.T) {
	for _, message := range []string{"NO_LINES", "INVALID_DIRECTION", "CLASSIFICATION_REQUIRED", "UNKNOWN_DIMENSION:branch", "line 1: invalid account"} {
		t.Run(message, func(t *testing.T) {
			finance := &invalidFinanceStub{message: message}
			jobs := &recordingJobGateway{}
			w := &ManualPostingWorkers{flow: SingleEntryFlow, financeClient: finance}
			job := entities.Job{ActivatedJob: &pb.ActivatedJob{Key: 42, Retries: 3, Variables: `{"journalEntryId":"pending-id","postingIdempotencyKey":"key","postingRequest":{"accountingDate":"2026-10-09","lines":[{"direction":"DEBIT","amountMinor":1,"accountCode":"1111"}]}}`}}
			w.validate()(jobs, job)
			if finance.released != 1 || jobs.failed != nil || jobs.validation == nil {
				t.Fatalf("released=%d failed=%v validation=%v", finance.released, jobs.failed, jobs.validation)
			}
			if jobs.validation.GetErrorCode() != ErrorValidationFailed || jobs.validation.GetErrorMessage() != message {
				t.Fatalf("validation=%v; want maker error with original reason", jobs.validation)
			}
		})
	}
}
