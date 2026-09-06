package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// CapitalService runs CFM fund flows: contract formation, capital receipt,
// fund disbursement — posting via the finance PostingService (rule card
// CFC_DISBURSEMENT / CFC_RECEIPT when dev_fac cards are appended).
type CapitalService struct {
	repo    *repository.CapitalRepository
	db      *sql.DB
	finance *financeclient.Client
}

func NewCapitalService(repo *repository.CapitalRepository, db *sql.DB, finance *financeclient.Client) *CapitalService {
	return &CapitalService{repo: repo, db: db, finance: finance}
}

// ListFundTypes returns active fund types.
func (s *CapitalService) ListFundTypes(ctx context.Context, tenantID string) ([]repository.FundType, error) {
	return s.repo.ListFundTypes(ctx, tenantID)
}

// ListContracts returns fund contracts.
func (s *CapitalService) ListContracts(ctx context.Context, tenantID string, orgCodes []string, status string) ([]repository.CapitalContract, error) {
	return s.repo.ListContracts(ctx, tenantID, orgCodes, status)
}

// CreateContract registers a fund contract.
func (s *CapitalService) CreateContract(ctx context.Context, tenantID, actor string, in *repository.CapitalContract) (*repository.CapitalContract, error) {
	if strings.TrimSpace(in.ContractCode) == "" || strings.TrimSpace(in.FundTypeCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "contract_code and fund_type_code are required")
	}
	if in.AmountMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "amount_minor must be positive")
	}
	in.TenantID = tenantID
	in.Status = "ACTIVE"
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	in.CreatedBy = actor
	created, err := s.repo.CreateContract(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// RecordMovement records + posts one fund movement (receipt or disbursement).
func (s *CapitalService) RecordMovement(ctx context.Context, tenantID, actor string, in *repository.CapitalMovement) (*repository.CapitalMovement, error) {
	if in.AmountMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "amount_minor must be positive")
	}
	contract, err := s.repo.GetContractByID(ctx, tenantID, in.ContractID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	if in.CurrencyCode == "" {
		in.CurrencyCode = contract.CurrencyCode
	}
	in.TenantID = tenantID
	in.Status = "DRAFT"
	in.CreatedBy = actor
	created, err := s.repo.RecordMovement(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}

	if s.finance != nil {
		docType := "CFC_RECEIPT"
		debitClass, creditClass := "CASH_SETTLEMENT_ACCOUNT", "FUND_CAPITAL_LIABILITY"
		if in.MovementType == "DISBURSEMENT" {
			docType = "CFC_DISBURSEMENT"
			debitClass, creditClass = "FUND_CAPITAL_LIABILITY", "CASH_SETTLEMENT_ACCOUNT"
		}
		postReq := &financev1.PostingRequest{
			IdempotencyKey: fmt.Sprintf("cfc-%s-%s", strings.ToLower(in.MovementType), created.ID),
			AccountingDate: in.MovementDate,
			CurrencyCode:   in.CurrencyCode,
			Description:    fmt.Sprintf("%s %s", in.MovementType, contract.ContractCode),
			BusinessReference: &financev1.BusinessReference{
				Domain:       "cfc",
				DocumentType: docType,
				DocumentId:   created.ID,
				DocumentCode: contract.ContractCode,
			},
			Lines: []*financev1.PostingLine{
				{
					LineNo:       1,
					Direction:    "DEBIT",
					AmountMinor:  in.AmountMinor,
					CurrencyCode: in.CurrencyCode,
					Analytics: &financev1.Analytics{
						AccClassification: debitClass,
						OrgUnitCode:       contract.OrgCode,
						FundSourceCode:    contract.FundTypeCode,
						Dimensions:        map[string]string{"contract_code": contract.ContractCode},
					},
				},
				{
					LineNo:       2,
					Direction:    "CREDIT",
					AmountMinor:  in.AmountMinor,
					CurrencyCode: in.CurrencyCode,
					Analytics: &financev1.Analytics{
						AccClassification: creditClass,
						OrgUnitCode:       contract.OrgCode,
						FundSourceCode:    contract.FundTypeCode,
						Dimensions:        map[string]string{"contract_code": contract.ContractCode},
					},
				},
			},
		}
		posted, err := s.finance.Post(ctx, postReq)
		if err != nil {
			return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "capital posting failed", err)
		}
		if err := s.repo.SetMovementJournal(ctx, tenantID, created.ID, "POSTED", posted.GetJournalEntryId()); err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
		}
		created.Status = "POSTED"
		created.JournalEntryID = &posted.JournalEntryId
	}
	return created, nil
}
