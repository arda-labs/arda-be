package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

type CustomerAmendment struct {
	ID             string         `json:"id"`
	CustomerID     string         `json:"customerId"`
	WorkflowCaseID string         `json:"workflowCaseId,omitempty"`
	Status         string         `json:"status"`
	BeforeSnapshot map[string]any `json:"beforeSnapshot,omitempty"`
	AfterSnapshot  map[string]any `json:"afterSnapshot,omitempty"`
	ChangedFields  []string       `json:"changedFields,omitempty"`
	AppliedAt      *time.Time     `json:"appliedAt,omitempty"`
	AppliedBy      string         `json:"appliedBy,omitempty"`
	RejectedAt     *time.Time     `json:"rejectedAt,omitempty"`
	RejectedBy     string         `json:"rejectedBy,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type AmendmentUpsert struct {
	AfterSnapshot map[string]any `json:"afterSnapshot"`
	ChangedFields []string       `json:"changedFields"`
}

type AmendmentRepository struct {
	db *sql.DB
}

func NewAmendmentRepository(db *sql.DB) *AmendmentRepository {
	return &AmendmentRepository{db: db}
}

func (r *AmendmentRepository) HasPending(ctx context.Context, customerID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM customer_amendments
			WHERE customer_id = $1 AND status IN ('DRAFT', 'PENDING')
		)
	`, customerID).Scan(&exists)
	return exists, err
}

func (r *AmendmentRepository) CreateDraft(ctx context.Context, customerID, workflowCaseID string) (*CustomerAmendment, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO customer_amendments (id, customer_id, workflow_case_id, status)
		VALUES ($1, $2, $3, 'DRAFT')
		RETURNING id, customer_id, workflow_case_id, status,
		          before_snapshot, after_snapshot, changed_fields,
		          applied_at, applied_by, rejected_at, rejected_by,
		          created_at, updated_at
	`, id, customerID, workflowCaseID)
	return scanAmendment(row)
}

func (r *AmendmentRepository) Get(ctx context.Context, id string) (*CustomerAmendment, error) {
	row := r.db.QueryRowContext(ctx, amendmentSelect()+` WHERE id = $1`, id)
	item, err := scanAmendment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *AmendmentRepository) GetPendingByCustomer(ctx context.Context, customerID string) (*CustomerAmendment, error) {
	row := r.db.QueryRowContext(ctx, amendmentSelect()+`
		WHERE customer_id = $1 AND status IN ('DRAFT', 'PENDING')
		ORDER BY updated_at DESC
		LIMIT 1
	`, customerID)
	item, err := scanAmendment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *AmendmentRepository) UpdateDraft(ctx context.Context, id string, in AmendmentUpsert) (*CustomerAmendment, error) {
	after, err := marshalMap(in.AfterSnapshot)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE customer_amendments
		SET after_snapshot = $2,
		    changed_fields = $3,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'DRAFT'
		RETURNING id, customer_id, workflow_case_id, status,
		          before_snapshot, after_snapshot, changed_fields,
		          applied_at, applied_by, rejected_at, rejected_by,
		          created_at, updated_at
	`, id, after, pqStringArray(in.ChangedFields))
	return scanAmendment(row)
}

func (r *AmendmentRepository) Submit(ctx context.Context, id, actor string, before map[string]any) (*CustomerAmendment, error) {
	beforeJSON, err := marshalMap(before)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var customerID string
	err = tx.QueryRowContext(ctx, `
		UPDATE customer_amendments
		SET status = 'PENDING',
		    before_snapshot = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'DRAFT'
		RETURNING customer_id
	`, id, beforeJSON).Scan(&customerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("amendment not found or not in DRAFT status")
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET status = 'PENDING_AMENDMENT', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'ACTIVE'
	`, customerID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *AmendmentRepository) GetPendingForWorkflow(ctx context.Context, customerID string) (*CustomerAmendment, error) {
	row := r.db.QueryRowContext(ctx, amendmentSelect()+`
		WHERE customer_id = $1 AND status = 'PENDING'
		ORDER BY updated_at DESC
		LIMIT 1
	`, customerID)
	item, err := scanAmendment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *AmendmentRepository) Apply(ctx context.Context, customerID, actor string) error {
	amendment, err := r.GetPendingForWorkflow(ctx, customerID)
	if err != nil || amendment == nil {
		return errors.New("no pending amendment")
	}
	return r.applyAmendment(ctx, amendment, actor, false)
}

func (r *AmendmentRepository) CancelDraft(ctx context.Context, id, customerID string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM customer_amendments
		WHERE id = $1 AND customer_id = $2 AND status = 'DRAFT'
	`, id, customerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("amendment not found or not in DRAFT status")
	}
	return nil
}

func (r *AmendmentRepository) Discard(ctx context.Context, customerID, actor string) error {
	amendment, err := r.GetPendingForWorkflow(ctx, customerID)
	if err != nil || amendment == nil {
		return errors.New("no pending amendment")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE customer_amendments
		SET status = 'REJECTED',
		    rejected_at = CURRENT_TIMESTAMP,
		    rejected_by = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, amendment.ID, actor); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, customerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AmendmentRepository) applyAmendment(ctx context.Context, amendment *CustomerAmendment, actor string, fromWorker bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	snap := amendment.AfterSnapshot
	name := stringField(snap, "name")
	email := stringField(snap, "email")
	mobile := stringField(snap, "mobile")
	identityNo := stringField(snap, "identityNo")
	address := stringField(snap, "address")
	personal, _ := marshalMap(mapField(snap, "personalInfo"))
	business, _ := marshalMap(mapField(snap, "businessInfo"))
	extended, _ := marshalMap(mapField(snap, "extendedInfo"))

	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET name = COALESCE(NULLIF($2, ''), name),
		    email = COALESCE(NULLIF($3, ''), email),
		    mobile = COALESCE(NULLIF($4, ''), mobile),
		    identity_no = COALESCE(NULLIF($5, ''), identity_no),
		    address = COALESCE(NULLIF($6, ''), address),
		    personal_info = CASE WHEN $7::jsonb = '{}'::jsonb THEN personal_info ELSE $7 END,
		    business_info = CASE WHEN $8::jsonb = '{}'::jsonb THEN business_info ELSE $8 END,
		    extended_info = CASE WHEN $9::jsonb = '{}'::jsonb THEN extended_info ELSE $9 END,
		    status = 'ACTIVE',
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, amendment.CustomerID, name, email, mobile, identityNo, address, personal, business, extended); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customer_amendments
		SET status = 'APPLIED',
		    applied_at = CURRENT_TIMESTAMP,
		    applied_by = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, amendment.ID, actor); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AmendmentRepository) ReopenPending(ctx context.Context, customerID string) error {
	amendment, err := r.GetPendingForWorkflow(ctx, customerID)
	if err != nil || amendment == nil {
		return errors.New("no pending amendment")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE customer_amendments
		SET status = 'DRAFT',
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'PENDING'
	`, amendment.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'PENDING_AMENDMENT'
	`, customerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *CustomerRepository) SetPendingAmendment(ctx context.Context, customerID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE customers SET status = 'PENDING_AMENDMENT', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'ACTIVE'
	`, customerID)
	return err
}

func CustomerSnapshot(c *Customer) map[string]any {
	if c == nil {
		return map[string]any{}
	}
	return map[string]any{
		"name":         c.Name,
		"email":        c.Email,
		"mobile":       c.Mobile,
		"identityNo":   c.IdentityNo,
		"address":      c.Address,
		"customerType": c.CustomerType,
		"personalInfo": c.PersonalInfo,
		"businessInfo": c.BusinessInfo,
		"extendedInfo": c.ExtendedInfo,
	}
}

func amendmentSelect() string {
	return `
		SELECT id, customer_id, workflow_case_id, status,
		       before_snapshot, after_snapshot, changed_fields,
		       applied_at, applied_by, rejected_at, rejected_by,
		       created_at, updated_at
		FROM customer_amendments`
}

func scanAmendment(row interface {
	Scan(dest ...any) error
}) (*CustomerAmendment, error) {
	var item CustomerAmendment
	var beforeRaw, afterRaw []byte
	var changed pq.StringArray
	var appliedAt, rejectedAt sql.NullTime
	var appliedBy, rejectedBy sql.NullString
	if err := row.Scan(
		&item.ID, &item.CustomerID, &item.WorkflowCaseID, &item.Status,
		&beforeRaw, &afterRaw, &changed,
		&appliedAt, &appliedBy, &rejectedAt, &rejectedBy,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.BeforeSnapshot = decodeMap(beforeRaw)
	item.AfterSnapshot = decodeMap(afterRaw)
	item.ChangedFields = changed
	if appliedAt.Valid {
		item.AppliedAt = &appliedAt.Time
	}
	if appliedBy.Valid {
		item.AppliedBy = appliedBy.String
	}
	if rejectedAt.Valid {
		item.RejectedAt = &rejectedAt.Time
	}
	if rejectedBy.Valid {
		item.RejectedBy = rejectedBy.String
	}
	return &item, nil
}

func pqStringArray(values []string) interface{} {
	if len(values) == 0 {
		return "{}"
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf(`"%s"`, strings.ReplaceAll(v, `"`, `\"`))
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func mapField(m map[string]any, key string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func decodeMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}
