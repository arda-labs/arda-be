package service

import (
	"context"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
)

// accrualStore is the storage the daily accrual (DPM.305) reads and writes. The
// repository implements it; the unit tests of RunDaily use an in-memory fake, so
// the state of the run lives in stored rows only, never in memory.
type accrualStore interface {
	ListActiveSavingsForAccrual(ctx context.Context, tenantID string) ([]repository.Savings, error)
	FirstPendingAccrual(ctx context.Context, tenantID, savingsID string) (*repository.Accrual, error)
	LastAccrualPeriod(ctx context.Context, tenantID, savingsID string) (string, error)
	InterestPeriodStart(ctx context.Context, tenantID, savingsID string) (string, error)
	SumPeriodAccruals(ctx context.Context, tenantID, savingsID, periodStart string) (int64, error)
	ListPostedTopUps(ctx context.Context, tenantID, savingsID, from string) ([]repository.TopUp, error)
	FindEffectiveRate(ctx context.Context, tenantID, productCode string, termMonths int, onDate string) (*repository.InterestRate, error)
	ListRateChangeDates(ctx context.Context, tenantID, productCode string, termMonths int, after, through string) ([]string, error)
	GetProductByCode(ctx context.Context, tenantID, code string) (*repository.SavingsProduct, error)
	CreateAccrual(ctx context.Context, a *repository.Accrual) (bool, error)
	MarkAccrualPosted(ctx context.Context, tenantID, id, journalEntryID string) error
}

var _ accrualStore = (*repository.DepositRepository)(nil)
