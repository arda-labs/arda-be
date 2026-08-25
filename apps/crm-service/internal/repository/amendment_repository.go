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

func (r *AmendmentRepository) HasPendingScoped(ctx context.Context, scope CustomerScope, customerID string) (bool, error) {
	if err := validateCustomerScope(scope); err != nil {
		return false, err
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM customer_amendments a
			WHERE a.customer_id = $2 AND a.status IN ('DRAFT', 'PENDING')
			  AND EXISTS (
				SELECT 1 FROM customers c
				WHERE c.id = a.customer_id AND c.tenant_id = $1 AND c.org_id = ANY($3)
			  )
		)
	`, scope.TenantID, customerID, pq.Array(scope.OrgIDs)).Scan(&exists)
	return exists, err
}

func (r *AmendmentRepository) CreateDraftScoped(ctx context.Context, scope CustomerScope, customerID, workflowCaseID string) (*CustomerAmendment, error) {
	if err := validateCustomerScope(scope); err != nil {
		return nil, err
	}
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO customer_amendments (id, customer_id, workflow_case_id, status)
		SELECT $1, c.id, $3, 'DRAFT'
		FROM customers c
		WHERE c.id = $2 AND c.tenant_id = $4 AND c.org_id = ANY($5)
		RETURNING id, customer_id, workflow_case_id, status,
		          before_snapshot, after_snapshot, changed_fields,
		          applied_at, applied_by, rejected_at, rejected_by,
		          created_at, updated_at
	`, id, customerID, workflowCaseID, scope.TenantID, pq.Array(scope.OrgIDs))
	return scanAmendment(row)
}

func (r *AmendmentRepository) GetScoped(ctx context.Context, scope CustomerScope, id string) (*CustomerAmendment, error) {
	if err := validateCustomerScope(scope); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, amendmentSelect()+` a WHERE a.id = $2
		AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = a.customer_id AND c.tenant_id = $1 AND c.org_id = ANY($3)
		)`, scope.TenantID, id, pq.Array(scope.OrgIDs))
	item, err := scanAmendment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *AmendmentRepository) GetPendingByCustomerScoped(ctx context.Context, scope CustomerScope, customerID string) (*CustomerAmendment, error) {
	if err := validateCustomerScope(scope); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, amendmentSelect()+`
		a WHERE a.customer_id = $2 AND a.status IN ('DRAFT', 'PENDING')
		  AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = a.customer_id AND c.tenant_id = $1 AND c.org_id = ANY($3)
		  )
		ORDER BY updated_at DESC
		LIMIT 1
	`, scope.TenantID, customerID, pq.Array(scope.OrgIDs))
	item, err := scanAmendment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *AmendmentRepository) UpdateDraftScoped(ctx context.Context, scope CustomerScope, id string, in AmendmentUpsert) (*CustomerAmendment, error) {
	if err := validateCustomerScope(scope); err != nil {
		return nil, err
	}
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
		  AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = customer_amendments.customer_id AND c.tenant_id = $4 AND c.org_id = ANY($5)
		  )
		RETURNING id, customer_id, workflow_case_id, status,
		          before_snapshot, after_snapshot, changed_fields,
		          applied_at, applied_by, rejected_at, rejected_by,
		          created_at, updated_at
	`, id, after, pqStringArray(in.ChangedFields), scope.TenantID, pq.Array(scope.OrgIDs))
	return scanAmendment(row)
}

func (r *AmendmentRepository) SubmitScoped(ctx context.Context, scope CustomerScope, id, actor string, before map[string]any) (*CustomerAmendment, error) {
	if err := validateCustomerScope(scope); err != nil {
		return nil, err
	}
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
		  AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = customer_amendments.customer_id AND c.tenant_id = $3 AND c.org_id = ANY($4)
		  )
		RETURNING customer_id
	`, id, beforeJSON, scope.TenantID, pq.Array(scope.OrgIDs)).Scan(&customerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("amendment not found or not in DRAFT status")
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET status = 'PENDING_AMENDMENT', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND tenant_id = $2 AND org_id = ANY($3) AND status = 'ACTIVE'
	`, customerID, scope.TenantID, pq.Array(scope.OrgIDs)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetScoped(ctx, scope, id)
}

func (r *AmendmentRepository) GetPendingForWorkflowScoped(ctx context.Context, scope CustomerScope, customerID string) (*CustomerAmendment, error) {
	if err := validateCustomerScope(scope); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, amendmentSelect()+`
		a WHERE a.customer_id = $2 AND a.status = 'PENDING'
		  AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = a.customer_id AND c.tenant_id = $1 AND c.org_id = ANY($3)
		  )
		ORDER BY updated_at DESC
		LIMIT 1
	`, scope.TenantID, customerID, pq.Array(scope.OrgIDs))
	item, err := scanAmendment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *AmendmentRepository) ApplyScoped(ctx context.Context, scope CustomerScope, customerID, actor string) error {
	amendment, err := r.GetPendingForWorkflowScoped(ctx, scope, customerID)
	if err != nil || amendment == nil {
		return errors.New("no pending amendment")
	}
	return r.applyAmendment(ctx, scope, amendment, actor)
}

func (r *AmendmentRepository) CancelDraftScoped(ctx context.Context, scope CustomerScope, id, customerID string) error {
	if err := validateCustomerScope(scope); err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM customer_amendments
		WHERE id = $1 AND customer_id = $2 AND status = 'DRAFT'
		  AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = customer_amendments.customer_id AND c.tenant_id = $3 AND c.org_id = ANY($4)
		  )
	`, id, customerID, scope.TenantID, pq.Array(scope.OrgIDs))
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

func (r *AmendmentRepository) DiscardScoped(ctx context.Context, scope CustomerScope, customerID, actor string) error {
	amendment, err := r.GetPendingForWorkflowScoped(ctx, scope, customerID)
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
		  AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = customer_amendments.customer_id AND c.tenant_id = $3 AND c.org_id = ANY($4)
		  )
	`, amendment.ID, actor, scope.TenantID, pq.Array(scope.OrgIDs)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND tenant_id = $2 AND org_id = ANY($3)
	`, customerID, scope.TenantID, pq.Array(scope.OrgIDs)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AmendmentRepository) applyAmendment(ctx context.Context, scope CustomerScope, amendment *CustomerAmendment, actor string) error {
	if err := validateCustomerScope(scope); err != nil {
		return err
	}
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
		WHERE id = $1 AND tenant_id = $10 AND org_id = ANY($11)
	`, amendment.CustomerID, name, email, mobile, identityNo, address, personal, business, extended, scope.TenantID, pq.Array(scope.OrgIDs)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customer_amendments
		SET status = 'APPLIED',
		    applied_at = CURRENT_TIMESTAMP,
		    applied_by = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = customer_amendments.customer_id AND c.tenant_id = $3 AND c.org_id = ANY($4)
		  )
	`, amendment.ID, actor, scope.TenantID, pq.Array(scope.OrgIDs)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AmendmentRepository) ReopenPendingScoped(ctx context.Context, scope CustomerScope, customerID string) error {
	amendment, err := r.GetPendingForWorkflowScoped(ctx, scope, customerID)
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
		  AND EXISTS (
			SELECT 1 FROM customers c
			WHERE c.id = customer_amendments.customer_id AND c.tenant_id = $2 AND c.org_id = ANY($3)
		  )
	`, amendment.ID, scope.TenantID, pq.Array(scope.OrgIDs)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customers
		SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND tenant_id = $2 AND org_id = ANY($3) AND status = 'PENDING_AMENDMENT'
	`, customerID, scope.TenantID, pq.Array(scope.OrgIDs)); err != nil {
		return err
	}
	return tx.Commit()
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
