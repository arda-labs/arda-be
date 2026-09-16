package grpc

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	hrmv1 "github.com/arda-labs/arda/libs/go/arda-proto/hrm/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// normalizeRegistrationStatus upper-cases status values read from the
// database so rows written before the status normalization migration still
// match the workflow contract.
func normalizeRegistrationStatus(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// isActionableRegistrationStatus reports whether a registration may enter the
// review path (the value the validate/execute workflow callbacks expect).
func isActionableRegistrationStatus(raw string) bool {
	switch normalizeRegistrationStatus(raw) {
	case "SUBMITTED", "IN_REVIEW":
		return true
	default:
		return false
	}
}

// isSettleableRegistrationStatus accepts the actionable statuses plus
// APPROVED, so a settle retried after a lost response returns the same
// employee instead of failing the workflow.
func isSettleableRegistrationStatus(raw string) bool {
	switch normalizeRegistrationStatus(raw) {
	case "SUBMITTED", "IN_REVIEW", "APPROVED":
		return true
	default:
		return false
	}
}

// newEmployeeID mirrors the repository id format (prefix + 16 random bytes);
// on an upsert conflict PostgreSQL keeps the existing row id instead.
func newEmployeeID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure hrm employee id generation failed: " + err.Error())
	}
	return "emp_" + hex.EncodeToString(b[:])
}

// registrationPayload is the subset of hrm_employee_registrations.payload that
// maps onto hrm_employees columns (the HRM registration form contract).
type registrationPayload struct {
	EmployeeCode string `json:"employee_code"`
	FullName     string `json:"full_name"`
	OrgUnitID    string `json:"org_unit_id"`
	Assignments  []struct {
		PositionID string `json:"position_id"`
	} `json:"assignments"`
}

// registrationEmployee holds the employee columns derived from a registration.
type registrationEmployee struct {
	EmployeeCode string
	FullName     string
	OrgUnitID    *string
	PositionID   *string
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// employeeFromRegistration maps an approved registration onto hrm_employees.
// employee_code falls back to the registration code (unique per tenant) so a
// retried settle stays idempotent even when the payload omitted the code; the
// first assignment carrying a position feeds the employee position. Missing
// required values are reported instead of creating an empty employee.
func employeeFromRegistration(registrationCode, rawPayload string) (registrationEmployee, error) {
	employee := registrationEmployee{EmployeeCode: strings.TrimSpace(registrationCode)}
	if strings.TrimSpace(rawPayload) != "" {
		var payload registrationPayload
		if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
			return registrationEmployee{}, fmt.Errorf("registration payload is not valid json: %w", err)
		}
		if code := strings.TrimSpace(payload.EmployeeCode); code != "" {
			employee.EmployeeCode = code
		}
		employee.FullName = strings.TrimSpace(payload.FullName)
		employee.OrgUnitID = optionalString(payload.OrgUnitID)
		for _, assignment := range payload.Assignments {
			if position := optionalString(assignment.PositionID); position != nil {
				employee.PositionID = position
				break
			}
		}
	}
	if employee.EmployeeCode == "" {
		return registrationEmployee{}, errors.New("registration has no employee_code and no registration_code to derive one from")
	}
	if employee.FullName == "" {
		return registrationEmployee{}, errors.New("registration payload has no full_name; refusing to create an empty employee")
	}
	return employee, nil
}

func (s *EmployeeServer) CheckRegistration(ctx context.Context, req *hrmv1.CheckRegistrationRequest) (*hrmv1.CheckRegistrationResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	var currentStatus string
	err = s.db.QueryRowContext(ctx, `SELECT status FROM hrm_employee_registrations WHERE tenant_id = $1 AND id = $2`,
		tenantID, req.GetRegistrationId()).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return &hrmv1.CheckRegistrationResponse{Ok: false, Message: "registration not found"}, nil
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !isActionableRegistrationStatus(currentStatus) {
		return &hrmv1.CheckRegistrationResponse{Ok: false, Message: "status " + currentStatus + " is not actionable"}, nil
	}
	return &hrmv1.CheckRegistrationResponse{Ok: true}, nil
}

// SettleRegistration approves a registration and materializes its employee in
// one transaction. The employee is upserted on the (tenant_id, employee_code)
// unique key, so retries never create duplicates; the response always carries
// the employee id the workflow stores in its variables.
func (s *EmployeeServer) SettleRegistration(ctx context.Context, req *hrmv1.SettleRegistrationRequest) (*hrmv1.SettleRegistrationResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	registrationID := strings.TrimSpace(req.GetRegistrationId())
	if registrationID == "" {
		return nil, status.Error(codes.InvalidArgument, "registration_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer func() { _ = tx.Rollback() }()

	var (
		registrationCode string
		payload          string
		currentStatus    string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT registration_code, payload::text, status
		FROM hrm_employee_registrations
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, registrationID).Scan(&registrationCode, &payload, &currentStatus)
	if err == sql.ErrNoRows {
		return nil, status.Errorf(codes.NotFound, "registration %s not found", registrationID)
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !isSettleableRegistrationStatus(currentStatus) {
		return nil, status.Errorf(codes.FailedPrecondition,
			"registration %s status %s cannot be settled", registrationID, normalizeRegistrationStatus(currentStatus))
	}

	employee, err := employeeFromRegistration(registrationCode, payload)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	var employeeID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO hrm_employees (id, tenant_id, employee_code, full_name, org_unit_id, position_id, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'ACTIVE')
		ON CONFLICT (tenant_id, employee_code) DO UPDATE
		SET full_name = EXCLUDED.full_name,
		    org_unit_id = EXCLUDED.org_unit_id,
		    position_id = EXCLUDED.position_id,
		    updated_at = now()
		RETURNING id`,
		newEmployeeID(), tenantID, employee.EmployeeCode, employee.FullName, employee.OrgUnitID, employee.PositionID,
	).Scan(&employeeID)
	if err != nil {
		return nil, status.Error(codes.Internal, "upsert employee: "+err.Error())
	}

	if normalizeRegistrationStatus(currentStatus) != "APPROVED" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE hrm_employee_registrations
			SET status = 'APPROVED', updated_at = now()
			WHERE tenant_id = $1 AND id = $2`, tenantID, registrationID); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &hrmv1.SettleRegistrationResponse{Ok: true, EmployeeId: employeeID}, nil
}

func (s *EmployeeServer) RejectRegistration(ctx context.Context, req *hrmv1.RejectRegistrationRequest) (*hrmv1.RejectRegistrationResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	tag, err := s.db.ExecContext(ctx, `
		UPDATE hrm_employee_registrations
		SET status = 'REJECTED', updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('SUBMITTED','IN_REVIEW')`,
		tenantID, req.GetRegistrationId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return &hrmv1.RejectRegistrationResponse{Ok: false}, nil
	}
	return &hrmv1.RejectRegistrationResponse{Ok: true}, nil
}
