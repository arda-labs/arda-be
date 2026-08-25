package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
)

// ApprovalRepository persists approval requests and steps.
type ApprovalRepository struct {
	db *sql.DB
}

func NewApprovalRepository(db *sql.DB) *ApprovalRepository {
	return &ApprovalRepository{db: db}
}

func (r *ApprovalRepository) Create(ctx context.Context, a *domain.ApprovalRequest) error {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_approval_requests (tenant_id, request_type, ref_id, status, current_level, total_levels, maker_id, maker_note, amount, currency)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, created_at
	`, a.TenantID, a.RequestType, a.RefID, a.Status, a.CurrentLevel,
		a.TotalLevels, a.MakerID, a.MakerNote, a.Amount, a.Currency)
	return row.Scan(&a.ID, &a.CreatedAt)
}

func (r *ApprovalRepository) GetByID(ctx context.Context, tenantID, id string) (*domain.ApprovalRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, request_type, ref_id, status, current_level, total_levels,
		       maker_id, maker_note, amount, currency, created_at, updated_at, completed_at
		FROM fin_approval_requests WHERE tenant_id = $1 AND id = $2
	`, tenantID, id)
	return scanApprovalRequest(row)
}

func (r *ApprovalRepository) GetByRefID(ctx context.Context, tenantID, refID string) (*domain.ApprovalRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, request_type, ref_id, status, current_level, total_levels,
		       maker_id, maker_note, amount, currency, created_at, updated_at, completed_at
		FROM fin_approval_requests WHERE tenant_id = $1 AND ref_id = $2
	`, tenantID, refID)
	return scanApprovalRequest(row)
}

func (r *ApprovalRepository) ListPending(ctx context.Context, tenantID string, level int) ([]domain.ApprovalRequest, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, request_type, ref_id, status, current_level, total_levels,
		       maker_id, maker_note, amount, currency, created_at, updated_at, completed_at
		FROM fin_approval_requests
		WHERE tenant_id = $1 AND status LIKE 'PENDING%' AND current_level = $2
		ORDER BY created_at DESC LIMIT 100
	`, tenantID, level)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []domain.ApprovalRequest
	for rows.Next() {
		var a domain.ApprovalRequest
		if err := scanApprovalRequestRow(rows, &a); err != nil {
			return nil, err
		}
		requests = append(requests, a)
	}
	return requests, rows.Err()
}

func (r *ApprovalRepository) UpdateStatus(ctx context.Context, tenantID, id string, status domain.ApprovalStatus, currentLevel int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE fin_approval_requests SET status = $1, current_level = $2, updated_at = now() WHERE tenant_id = $3 AND id = $4
	`, status, currentLevel, tenantID, id)
	return err
}

func (r *ApprovalRepository) Complete(ctx context.Context, tenantID, id string, status domain.ApprovalStatus) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE fin_approval_requests SET status = $1, completed_at = now(), updated_at = now() WHERE tenant_id = $2 AND id = $3
	`, status, tenantID, id)
	return err
}

// ── Steps ──

func (r *ApprovalRepository) InsertStep(ctx context.Context, step *domain.ApprovalStep) error {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_approval_steps (request_id, level, checker_id, decision, note, decided_at)
		VALUES ($1,$2,$3,$4,$5, now()) RETURNING id, created_at
	`, step.RequestID, step.Level, step.CheckerID, step.Decision, step.Note)
	return row.Scan(&step.ID, &step.CreatedAt)
}

func (r *ApprovalRepository) GetSteps(ctx context.Context, tenantID, requestID string) ([]domain.ApprovalStep, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, request_id, level, checker_id, decision, note, decided_at, created_at
		FROM fin_approval_steps s
		JOIN fin_approval_requests a ON a.id = s.request_id
		WHERE a.tenant_id = $1 AND s.request_id = $2 ORDER BY level
	`, tenantID, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []domain.ApprovalStep
	for rows.Next() {
		var s domain.ApprovalStep
		if err := rows.Scan(&s.ID, &s.RequestID, &s.Level, &s.CheckerID, &s.Decision, &s.Note, &s.DecidedAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	return steps, rows.Err()
}

func scanApprovalRequest(row *sql.Row) (*domain.ApprovalRequest, error) {
	var a domain.ApprovalRequest
	if err := scanApprovalRequestRow(row, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func scanApprovalRequestRow(scanner interface{ Scan(dest ...any) error }, a *domain.ApprovalRequest) error {
	var makerNote, amount sql.NullString
	var updatedAt, completedAt sql.NullTime

	err := scanner.Scan(&a.ID, &a.TenantID, &a.RequestType, &a.RefID, &a.Status,
		&a.CurrentLevel, &a.TotalLevels, &a.MakerID, &makerNote, &amount,
		&a.Currency, &a.CreatedAt, &updatedAt, &completedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return err
	}
	a.MakerNote = makerNote.String
	a.Amount = amount.String
	if updatedAt.Valid {
		a.UpdatedAt = &updatedAt.Time
	}
	if completedAt.Valid {
		a.CompletedAt = &completedAt.Time
	}
	return nil
}

var _ = json.Marshal
var _ = strings.TrimSpace
