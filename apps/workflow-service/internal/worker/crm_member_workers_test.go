package worker

import (
	"errors"
	"testing"

	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsMemberTerminalError(t *testing.T) {
	// A stale optimistic-lock token (crm-service maps ErrMemberVersionConflict
	// to codes.Aborted) can never be fixed by retrying the same decision.
	if !isMemberTerminalError(status.Error(codes.Aborted, "version conflict")) {
		t.Fatal("Aborted must be terminal")
	}
	// Not found / already decided / malformed decision.
	if !isMemberTerminalError(status.Error(codes.FailedPrecondition, "member request not found")) {
		t.Fatal("FailedPrecondition must be terminal")
	}
	// Transport/infra failures are retryable.
	for _, code := range []codes.Code{codes.Unavailable, codes.Internal, codes.DeadlineExceeded, codes.Unknown} {
		if isMemberTerminalError(status.Error(code, "transient")) {
			t.Fatalf("%v must be retryable, not terminal", code)
		}
	}
	if isMemberTerminalError(nil) {
		t.Fatal("nil error must not be terminal")
	}
	if isMemberTerminalError(errors.New("plain error")) {
		t.Fatal("plain (non-status) error must be retryable")
	}
}

func TestMemberDecisionContext(t *testing.T) {
	// camunda-style numeric dataVersion arrives as float64.
	actor, version := memberDecisionContext(jobWithVars(`{"actor":"user-1","dataVersion":2}`))
	if actor != "user-1" || version != 2 {
		t.Fatalf("actor=%q version=%d, want user-1/2", actor, version)
	}
	// decisionBy is the fallback actor key; missing version stays 0 (no lock).
	actor, version = memberDecisionContext(jobWithVars(`{"decisionBy":"user-2"}`))
	if actor != "user-2" || version != 0 {
		t.Fatalf("actor=%q version=%d, want user-2/0", actor, version)
	}
}

func jobWithVars(variables string) entities.Job {
	return entities.Job{ActivatedJob: &pb.ActivatedJob{Variables: variables}}
}
