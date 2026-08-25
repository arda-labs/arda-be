package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

type TenantService struct {
	repo *repository.TenantRepository
}

func NewTenantService(repo *repository.TenantRepository) *TenantService {
	return &TenantService{repo: repo}
}

func (s *TenantService) List(ctx context.Context) ([]domain.Tenant, error) {
	return s.repo.List(ctx)
}

func (s *TenantService) ListForUser(ctx context.Context, userID string) ([]domain.TenantMembership, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user id is required")
	}
	return s.repo.ListForUser(ctx, userID)
}

func (s *TenantService) ListMembers(ctx context.Context, tenantID string) ([]domain.TenantMember, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("tenant id is required")
	}
	if exists, err := s.repo.Exists(ctx, tenantID); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("tenant does not exist or is not active")
	}
	return s.repo.ListMembers(ctx, tenantID)
}

func (s *TenantService) AddMember(ctx context.Context, tenantID, userID string, isDefault bool) error {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return fmt.Errorf("tenant id and user id are required")
	}
	return s.repo.EnsureMembership(ctx, userID, tenantID, isDefault)
}

func (s *TenantService) RemoveMember(ctx context.Context, tenantID, userID string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(userID) == "" {
		return fmt.Errorf("tenant id and user id are required")
	}
	return s.repo.RemoveMembership(ctx, tenantID, userID)
}

func (s *TenantService) Create(ctx context.Context, code, name, ownerUserID string) (*domain.Tenant, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, fmt.Errorf("tenant code and name are required")
	}
	if strings.EqualFold(code, "default") || strings.EqualFold(code, "system") {
		return nil, fmt.Errorf("reserved tenant code")
	}
	tenant := &domain.Tenant{Code: code, Name: name}
	if err := s.repo.Create(ctx, tenant, strings.TrimSpace(ownerUserID)); err != nil {
		return nil, err
	}
	return tenant, nil
}
