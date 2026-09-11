package service

import (
	"context"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
)

// ReportService serves the CRM report reads (W4c).
type ReportService struct {
	repo *repository.CustomerRepository
}

func NewReportService(repo *repository.CustomerRepository) *ReportService {
	return &ReportService{repo: repo}
}

// CustomerReport is the báo cáo khách hàng read.
func (s *ReportService) CustomerReport(ctx context.Context, tenantID, q, customerType, status string) ([]repository.CustomerReportRow, error) {
	return s.repo.CustomerReport(ctx, tenantID, q, customerType, status)
}
