package grpc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	hrmv1 "github.com/arda-labs/arda/libs/go/arda-proto/hrm/v1"
)

// EmployeeServer implements arda.hrm.v1.EmployeeCommandService — the
// callback surface workflow-service workers call for the
// hrm-employee-registration-v2 flow.
type EmployeeServer struct {
	hrmv1.UnimplementedEmployeeCommandServiceServer
	db *sql.DB
}

func NewEmployeeServer(db *sql.DB) *EmployeeServer {
	return &EmployeeServer{db: db}
}

func tenantFromContext(ctx context.Context) (string, error) {
	md := ardametadata.FromIncoming(ctx)
	tenantID := strings.TrimSpace(md.TenantID)
	if tenantID == "" {
		return "", fmt.Errorf("tenant scope is required")
	}
	return tenantID, nil
}

func (s *EmployeeServer) CheckRegistration(ctx context.Context, req *hrmv1.CheckRegistrationRequest) (*hrmv1.CheckRegistrationResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	var status string
	err = s.db.QueryRowContext(ctx, `SELECT status FROM hrm_employee_registrations WHERE tenant_id = $1 AND id = $2`,
		tenantID, req.GetRegistrationId()).Scan(&status)
	if err == sql.ErrNoRows {
		return &hrmv1.CheckRegistrationResponse{Ok: false, Message: "registration not found"}, nil
	}
	if err != nil {
		return nil, err
	}
	if status != "SUBMITTED" && status != "IN_REVIEW" {
		return &hrmv1.CheckRegistrationResponse{Ok: false, Message: "status " + status + " is not actionable"}, nil
	}
	return &hrmv1.CheckRegistrationResponse{Ok: true}, nil
}

func (s *EmployeeServer) SettleRegistration(ctx context.Context, req *hrmv1.SettleRegistrationRequest) (*hrmv1.SettleRegistrationResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tag, err := s.db.ExecContext(ctx, `
		UPDATE hrm_employee_registrations
		SET status = 'APPROVED', updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('SUBMITTED','IN_REVIEW')`,
		tenantID, req.GetRegistrationId())
	if err != nil {
		return nil, err
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return &hrmv1.SettleRegistrationResponse{Ok: false}, nil
	}
	return &hrmv1.SettleRegistrationResponse{Ok: true}, nil
}

func (s *EmployeeServer) RejectRegistration(ctx context.Context, req *hrmv1.RejectRegistrationRequest) (*hrmv1.RejectRegistrationResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tag, err := s.db.ExecContext(ctx, `
		UPDATE hrm_employee_registrations
		SET status = 'REJECTED', updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('SUBMITTED','IN_REVIEW')`,
		tenantID, req.GetRegistrationId())
	if err != nil {
		return nil, err
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return &hrmv1.RejectRegistrationResponse{Ok: false}, nil
	}
	return &hrmv1.RejectRegistrationResponse{Ok: true}, nil
}
