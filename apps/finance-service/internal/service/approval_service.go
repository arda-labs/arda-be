package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

// ApprovalRule defines approval tiers for a transaction type.
type ApprovalRule struct {
	Type  string
	Tiers []ApprovalTier
}

// ApprovalTier maps amount range to approval levels.
type ApprovalTier struct {
	MaxAmount float64
	Levels    int
}

// DefaultApprovalRules in production should come from DB/config.
var DefaultApprovalRules = []ApprovalRule{
	{
		Type: "TRANSFER",
		Tiers: []ApprovalTier{
			{MaxAmount: 100_000_000, Levels: 2},       // < 100tr: maker + checker
			{MaxAmount: 1_000_000_000, Levels: 3},     // 100tr - 1tỷ: + senior
			{MaxAmount: -1, Levels: 4},                // > 1tỷ: + director
		},
	},
	{
		Type: "DEPOSIT",
		Tiers: []ApprovalTier{
			{MaxAmount: 500_000_000, Levels: 2},
			{MaxAmount: -1, Levels: 3},
		},
	},
	{
		Type: "WITHDRAWAL",
		Tiers: []ApprovalTier{
			{MaxAmount: 50_000_000, Levels: 2},
			{MaxAmount: 500_000_000, Levels: 3},
			{MaxAmount: -1, Levels: 4},
		},
	},
	{
		Type: "ACCOUNT_OPENING",
		Tiers: []ApprovalTier{
			{MaxAmount: -1, Levels: 2},
		},
	},
}

// ApprovalService handles the maker-checker approval workflow.
type ApprovalService struct {
	approvalRepo *repository.ApprovalRepository
	txnRepo      *repository.TransactionRepository
	rules        []ApprovalRule
	logger       *slog.Logger
}

func NewApprovalService(approvalRepo *repository.ApprovalRepository, txnRepo *repository.TransactionRepository, rules []ApprovalRule) *ApprovalService {
	if rules == nil {
		rules = DefaultApprovalRules
	}
	return &ApprovalService{
		approvalRepo: approvalRepo,
		txnRepo:      txnRepo,
		rules:        rules,
		logger:       slog.Default(),
	}
}

// DetermineLevels returns how many approval levels are needed for a transaction.
func (s *ApprovalService) DetermineLevels(txnType string, amount float64) int {
	for _, rule := range s.rules {
		if rule.Type == txnType {
			for _, tier := range rule.Tiers {
				if tier.MaxAmount < 0 || amount <= tier.MaxAmount {
					return tier.Levels
				}
			}
		}
	}
	return 2 // default: 2 levels
}

// CreateApproval creates an approval request for a transaction.
// The transaction stays PENDING until all levels approve.
func (s *ApprovalService) CreateApproval(ctx context.Context, tenantID, requestType, refID, makerID, makerNote, amount string) (*domain.ApprovalRequest, error) {
	amt := parseDecimal(amount)
	levels := s.DetermineLevels(requestType, amt)

	req := &domain.ApprovalRequest{
		TenantID:     tenantID,
		RequestType:  requestType,
		RefID:        refID,
		Status:       domain.ApprovalPending,
		CurrentLevel: 1,
		TotalLevels:  levels,
		MakerID:      makerID,
		MakerNote:    makerNote,
		Amount:       amount,
		Currency:     "VND",
	}

	if err := s.approvalRepo.Create(ctx, req); err != nil {
		return nil, fmt.Errorf("create approval: %w", err)
	}

	s.logger.Info("approval created",
		"id", req.ID, "type", requestType,
		"ref", refID, "levels", levels,
	)

	return req, nil
}

// Approve records a checker's approval. If all levels done, marks APPROVED.
func (s *ApprovalService) Approve(ctx context.Context, requestID, checkerID, note string) (*domain.ApprovalRequest, error) {
	req, err := s.approvalRepo.GetByID(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("approval request not found")
	}
	if req.Status != domain.ApprovalPending && req.Status != domain.ApprovalPendingL2 && req.Status != domain.ApprovalPendingL3 {
		return nil, fmt.Errorf("approval request is not pending (status: %s)", req.Status)
	}

	level := req.CurrentLevel

	// Check if this checker can approve at this level (in production: check role/permission pool)
	step := &domain.ApprovalStep{
		RequestID: requestID,
		Level:     level,
		CheckerID: checkerID,
		Decision:  "APPROVED",
		Note:      note,
	}
	if err := s.approvalRepo.InsertStep(ctx, step); err != nil {
		return nil, fmt.Errorf("insert step: %w", err)
	}

	// Check if all levels done
	if level >= req.TotalLevels {
		if err := s.approvalRepo.Complete(ctx, requestID, domain.ApprovalApproved); err != nil {
			return nil, err
		}
		req.Status = domain.ApprovalApproved

		// Auto-post the transaction
		if err := s.txnRepo.UpdateStatus(ctx, req.RefID, domain.TxnPosted, checkerID); err != nil {
			s.logger.Warn("auto-post transaction failed", "ref", req.RefID, "err", err)
		} else {
			s.logger.Info("transaction auto-posted after approval", "ref", req.RefID)
		}
	} else {
		nextStatus := domain.ApprovalPendingL2
		if level+1 == 3 {
			nextStatus = domain.ApprovalPendingL3
		}
		if err := s.approvalRepo.UpdateStatus(ctx, requestID, nextStatus, level+1); err != nil {
			return nil, err
		}
		req.Status = nextStatus
		req.CurrentLevel = level + 1
	}

	req.Steps, _ = s.approvalRepo.GetSteps(ctx, requestID)
	return req, nil
}

// Reject records a rejection. It's final — no more approvals possible.
func (s *ApprovalService) Reject(ctx context.Context, requestID, checkerID, note string) (*domain.ApprovalRequest, error) {
	req, err := s.approvalRepo.GetByID(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("approval request not found")
	}

	step := &domain.ApprovalStep{
		RequestID: requestID,
		Level:     req.CurrentLevel,
		CheckerID: checkerID,
		Decision:  "REJECTED",
		Note:      note,
	}
	if err := s.approvalRepo.InsertStep(ctx, step); err != nil {
		return nil, err
	}

	if err := s.approvalRepo.Complete(ctx, requestID, domain.ApprovalRejected); err != nil {
		return nil, err
	}
	req.Status = domain.ApprovalRejected

	// Mark transaction as FAILED
	s.txnRepo.UpdateStatus(ctx, req.RefID, domain.TxnFailed, checkerID)

	s.logger.Info("approval rejected", "id", requestID, "by", checkerID)
	return req, nil
}

// Cancel allows the maker to cancel a pending approval.
func (s *ApprovalService) Cancel(ctx context.Context, requestID, userID string) (*domain.ApprovalRequest, error) {
	req, err := s.approvalRepo.GetByID(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("approval request not found")
	}
	if req.MakerID != userID {
		return nil, fmt.Errorf("only the maker can cancel this request")
	}
	if req.Status != domain.ApprovalPending {
		return nil, fmt.Errorf("can only cancel pending requests")
	}

	if err := s.approvalRepo.Complete(ctx, requestID, domain.ApprovalCancelled); err != nil {
		return nil, err
	}
	req.Status = domain.ApprovalCancelled

	s.txnRepo.UpdateStatus(ctx, req.RefID, domain.TxnFailed, userID)
	return req, nil
}

// ListPending returns pending approvals for a given level.
func (s *ApprovalService) ListPending(ctx context.Context, tenantID string, level int) ([]domain.ApprovalRequest, error) {
	return s.approvalRepo.ListPending(ctx, tenantID, level)
}

// GetApproval returns a full approval request with steps.
func (s *ApprovalService) GetApproval(ctx context.Context, id string) (*domain.ApprovalRequest, error) {
	req, err := s.approvalRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	steps, err := s.approvalRepo.GetSteps(ctx, id)
	if err == nil {
		req.Steps = steps
	}
	return req, nil
}
