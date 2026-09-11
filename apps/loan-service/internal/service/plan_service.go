package service

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// PlanService serves the loan plan catalog (W7).
type PlanService struct {
	repo *repository.LoanRepository
}

func NewPlanService(repo *repository.LoanRepository) *PlanService {
	return &PlanService{repo: repo}
}

// ListPlans returns the plan catalog.
func (s *PlanService) ListPlans(ctx context.Context, tenantID, status string) ([]repository.LoanPlan, error) {
	return s.repo.ListPlans(ctx, tenantID, status)
}

// UpsertPlan creates or updates one plan.
func (s *PlanService) UpsertPlan(ctx context.Context, tenantID, actor string, in *repository.LoanPlan) (*repository.LoanPlan, error) {
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	in.TenantID = tenantID
	in.ID = ""
	in.CreatedBy = actor
	created, err := s.repo.UpsertPlan(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// ClosePlan marks one plan CLOSED.
func (s *PlanService) ClosePlan(ctx context.Context, tenantID, id string) error {
	if err := s.repo.SetPlanStatus(ctx, tenantID, id, "CLOSED"); err != nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, "plan not found")
	}
	return nil
}
