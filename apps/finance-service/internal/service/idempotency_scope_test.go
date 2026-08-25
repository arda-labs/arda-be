package service

import (
	"context"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
)

func TestFinanceOperationRejectsMissingTenantBeforeRepositoryAccess(t *testing.T) {
	svc := NewFinanceOperationService(nil, nil, nil)
	_, err := svc.CreateIncoming(context.Background(), "", OperationCreateRequest{
		IdempotencyKey: "request-1",
		TxnType:        "TRANSFER",
		Amount:         "100",
		CreatedBy:      "actor-1",
	})
	if err == nil || !strings.Contains(err.Error(), "tenant id required") {
		t.Fatalf("error = %v, want explicit tenant scope failure", err)
	}
}

func TestFinanceOperationRejectsMissingActorBeforeRepositoryAccess(t *testing.T) {
	svc := NewFinanceOperationService(nil, nil, nil)
	_, err := svc.CreateIncoming(context.Background(), "tenant-1", OperationCreateRequest{
		IdempotencyKey: "request-1",
		TxnType:        "TRANSFER",
		Amount:         "100",
	})
	if err == nil || !strings.Contains(err.Error(), "created by actor required") {
		t.Fatalf("error = %v, want explicit actor failure", err)
	}
}

func TestLedgerRejectsUnscopedTransactionBeforeRepositoryAccess(t *testing.T) {
	svc := NewLedgerService(nil, nil)
	_, err := svc.PostTransaction(context.Background(), &domain.Transaction{
		CreatedBy: "actor-1",
	})
	if err == nil || !strings.Contains(err.Error(), "tenant id required") {
		t.Fatalf("error = %v, want explicit tenant scope failure", err)
	}
}

func TestLedgerRejectsUnauthenticatedTransactionBeforeRepositoryAccess(t *testing.T) {
	svc := NewLedgerService(nil, nil)
	_, err := svc.PostTransaction(context.Background(), &domain.Transaction{
		TenantID: "tenant-1",
	})
	if err == nil || !strings.Contains(err.Error(), "created by actor required") {
		t.Fatalf("error = %v, want explicit actor failure", err)
	}
}
