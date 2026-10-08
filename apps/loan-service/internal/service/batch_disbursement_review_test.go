package service

import (
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

func TestCalculateBatchHeadroomAggregatesRowsByContract(t *testing.T) {
	rows := []domain.Disbursement{
		{ContractCode: "C-1", DisburseAmtMinor: 2_000},
		{ContractCode: "C-1", DisburseAmtMinor: 4_000},
		{ContractCode: "C-2", DisburseAmtMinor: 1_500},
	}
	exposures := map[string]domain.ContractExposure{
		"C-1": {LoanAmountMinor: 10_000, OutstandingMinor: 2_000, PendingMinor: 1_000},
		"C-2": {LoanAmountMinor: 8_000, OutstandingMinor: 500},
	}

	got := calculateBatchHeadroom(domain.BatchDraft, rows, exposures)
	if len(got) != 2 {
		t.Fatalf("got %d contracts, want 2", len(got))
	}
	if got[0].ContractCode != "C-1" || got[0].AvailableBefore != 7_000 || got[0].RemainingAfter != 1_000 {
		t.Fatalf("C-1 headroom = %+v, want before=7000 after=1000", got[0])
	}
	if got[1].ContractCode != "C-2" || got[1].AvailableBefore != 7_500 || got[1].RemainingAfter != 6_000 {
		t.Fatalf("C-2 headroom = %+v, want before=7500 after=6000", got[1])
	}
}

func TestCalculatePendingBatchHeadroomIncludesItsHeldReservation(t *testing.T) {
	rows := []domain.Disbursement{{ContractCode: "C-1", DisburseAmtMinor: 6_000}}
	exposures := map[string]domain.ContractExposure{
		"C-1": {LoanAmountMinor: 10_000, OutstandingMinor: 2_000, PendingMinor: 1_000, ReservedMinor: 6_000},
	}

	got := calculateBatchHeadroom(domain.BatchSubmitted, rows, exposures)
	if len(got) != 1 || got[0].AvailableBefore != 7_000 || got[0].RemainingAfter != 1_000 {
		t.Fatalf("pending batch headroom = %+v, want before=7000 after=1000", got)
	}
}
