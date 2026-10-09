package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

type AgreementDailySnapshotService struct {
	repo *repository.LoanRepository
}

func NewAgreementDailySnapshotService(repo *repository.LoanRepository) *AgreementDailySnapshotService {
	return &AgreementDailySnapshotService{repo: repo}
}

type AgreementDailySnapshotRun struct {
	TenantID  string `json:"tenant_id"`
	DataDate  string `json:"data_date"`
	Snapshots int64  `json:"snapshots"`
}

func (s *AgreementDailySnapshotService) Run(ctx context.Context, tenantID, dataDate string) (*AgreementDailySnapshotRun, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("tenant scope is required")
	}
	parsed, err := time.Parse("2006-01-02", dataDate)
	if err != nil || parsed.Format("2006-01-02") != dataDate {
		return nil, fmt.Errorf("to_date must be YYYY-MM-DD")
	}
	count, err := s.repo.CaptureAgreementDailySnapshots(ctx, tenantID, dataDate)
	if err != nil {
		return nil, err
	}
	return &AgreementDailySnapshotRun{TenantID: tenantID, DataDate: dataDate, Snapshots: count}, nil
}
