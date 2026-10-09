package service

import (
	"context"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

// Dossier builds the composite view for one contract.
func (s *LoanService) Dossier(ctx context.Context, tenantID, contractID string) (*repository.Dossier, error) {
	contract, err := s.repo.GetContract(ctx, tenantID, contractID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	agreements, err := s.repo.ListAgreements(ctx, tenantID, contract.ContractCode)
	if err != nil {
		return nil, mapRepoError(err)
	}
	plans, err := s.repo.ListRepayPlans(ctx, tenantID, contract.ContractCode, "")
	if err != nil {
		return nil, mapRepoError(err)
	}
	disbursements, _, err := s.repo.ListDisbursements(ctx, tenantID, nil, "", contract.ContractCode, "", "", "", "", 500, 0)
	if err != nil {
		return nil, mapRepoError(err)
	}
	collections, _, err := s.repo.ListCollections(ctx, tenantID, nil, "", contract.ContractCode, "", "", "", 500, 0)
	if err != nil {
		return nil, mapRepoError(err)
	}
	mortgages, err := s.repo.ListMortgages(ctx, tenantID, contract.ContractCode)
	if err != nil {
		return nil, mapRepoError(err)
	}
	var collaterals []domain.Collateral
	for _, m := range mortgages {
		rows, err := s.repo.ListCollaterals(ctx, tenantID, m.MortgageCode, "")
		if err != nil {
			return nil, mapRepoError(err)
		}
		collaterals = append(collaterals, rows...)
	}
	caseIDs, err := s.repo.ListContractCaseIDs(ctx, tenantID, contract.ID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return &repository.Dossier{
		Contract:      contract,
		Agreements:    agreements,
		RepayPlans:    plans,
		Disbursements: disbursements,
		Collections:   collections,
		Mortgages:     mortgages,
		Collaterals:   collaterals,
		CaseIDs:       caseIDs,
	}, nil
}

// OperationMetrics returns the TT92 performance aggregates finance-service
// reads for PLIIb: collection volume in [fromDate, toDate] and the current
// non-performing balance (debt groups 3-5).
func (s *LoanService) OperationMetrics(ctx context.Context, tenantID, fromDate, toDate string) (int64, int64, error) {
	volume, err := s.repo.CollectionVolume(ctx, tenantID, fromDate, toDate)
	if err != nil {
		return 0, 0, err
	}
	npl, err := s.repo.NPLBalance(ctx, tenantID)
	if err != nil {
		return 0, 0, err
	}
	return volume, npl, nil
}
