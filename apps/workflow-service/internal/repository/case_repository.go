package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CaseStatusDraft     = "DRAFT"
	CaseStatusSubmitted = "SUBMITTED"
	CaseStatusInReview  = "IN_REVIEW"
	CaseStatusCompleted = "COMPLETED"
)

var ErrNotFound = errors.New("not found")

type CaseType struct {
	CaseType           string     `json:"caseType"`
	BusinessArea       string     `json:"businessArea"`
	OperationName      string     `json:"operationName"`
	BpmnProcessID      string     `json:"bpmnProcessId"`
	BpmnVersion        int        `json:"bpmnVersion"`
	WorkflowEnabled    bool       `json:"workflowEnabled"`
	DefaultSLAPolicyID *string    `json:"defaultSlaPolicyId,omitempty"`
	MakerRole          string     `json:"makerRole"`
	CheckerRole        string     `json:"checkerRole"`
	OwnerService       string     `json:"ownerService"`
	Status             string     `json:"status"`
	EffectiveFrom      time.Time  `json:"effectiveFrom"`
	EffectiveTo        *time.Time `json:"effectiveTo,omitempty"`
}

type CaseTypeUpsert struct {
	CaseType           string `json:"caseType"`
	BusinessArea       string `json:"businessArea"`
	OperationName      string `json:"operationName"`
	BpmnProcessID      string `json:"bpmnProcessId"`
	BpmnVersion        int    `json:"bpmnVersion"`
	WorkflowEnabled    bool   `json:"workflowEnabled"`
	DefaultSLAPolicyID string `json:"defaultSlaPolicyId"`
	MakerRole          string `json:"makerRole"`
	CheckerRole        string `json:"checkerRole"`
	OwnerService       string `json:"ownerService"`
	Status             string `json:"status"`
}

type BusinessCase struct {
	ID                 string     `json:"id"`
	TenantID           string     `json:"tenantId"`
	CaseType           string     `json:"caseType"`
	CaseCode           string     `json:"caseCode"`
	Title              string     `json:"title"`
	PrimaryObjectType  string     `json:"primaryObjectType"`
	PrimaryObjectID    string     `json:"primaryObjectId"`
	DomainService      string     `json:"domainService"`
	Status             string     `json:"status"`
	CurrentStep        string     `json:"currentStep"`
	Priority           string     `json:"priority"`
	CreatedBy          string     `json:"createdBy"`
	AssignedTo         *string    `json:"assignedTo,omitempty"`
	CandidateRole      *string    `json:"candidateRole,omitempty"`
	SLAPolicyID        *string    `json:"slaPolicyId,omitempty"`
	SLADueAt           *time.Time `json:"slaDueAt,omitempty"`
	ProcessInstanceKey *int64     `json:"processInstanceKey,omitempty"`
	BpmnProcessID      *string    `json:"bpmnProcessId,omitempty"`
	BpmnVersion        *int       `json:"bpmnVersion,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
}

type CaseCreate struct {
	TenantID          string `json:"tenantId"`
	CaseType          string `json:"caseType"`
	CaseCode          string `json:"caseCode"`
	Title             string `json:"title"`
	PrimaryObjectType string `json:"primaryObjectType"`
	PrimaryObjectID   string `json:"primaryObjectId"`
	DomainService     string `json:"domainService"`
	Priority          string `json:"priority"`
	CreatedBy         string `json:"createdBy"`
}

type CaseListFilter struct {
	CaseType      string
	Status        string
	AssignedTo    string
	CandidateRole string
	Keyword       string
	Limit         int
}

type TimelineEvent struct {
	ID         int64           `json:"id"`
	CaseID     string          `json:"caseId"`
	EventType  string          `json:"eventType"`
	FromStatus *string         `json:"fromStatus,omitempty"`
	ToStatus   *string         `json:"toStatus,omitempty"`
	Actor      *string         `json:"actor,omitempty"`
	Note       string          `json:"note"`
	Data       json.RawMessage `json:"data"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type CaseRepository struct {
	db *sql.DB
}

func NewCaseRepository(db *sql.DB) *CaseRepository {
	return &CaseRepository{db: db}
}

func (r *CaseRepository) ListCaseTypes(ctx context.Context) ([]CaseType, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
		       workflow_enabled, default_sla_policy_id, maker_role, checker_role,
		       owner_service, status, effective_from, effective_to
		FROM business_operation_types
		ORDER BY business_area, operation_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CaseType
	for rows.Next() {
		ct, err := scanCaseType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ct)
	}
	return out, rows.Err()
}

func (r *CaseRepository) CreateCaseType(ctx context.Context, in CaseTypeUpsert) (*CaseType, error) {
	if err := validateCaseTypeUpsert(in, true); err != nil {
		return nil, err
	}
	if in.BpmnVersion <= 0 {
		in.BpmnVersion = 1
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO business_operation_types (
			case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
			workflow_enabled, default_sla_policy_id, maker_role, checker_role, owner_service, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10, $11)
		RETURNING case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
		          workflow_enabled, default_sla_policy_id, maker_role, checker_role,
		          owner_service, status, effective_from, effective_to
	`, in.CaseType, in.BusinessArea, in.OperationName, in.BpmnProcessID, in.BpmnVersion,
		in.WorkflowEnabled, in.DefaultSLAPolicyID, in.MakerRole, in.CheckerRole,
		in.OwnerService, statusOrActive(in.Status))
	ct, err := scanCaseType(row)
	return &ct, err
}

func (r *CaseRepository) UpdateCaseType(ctx context.Context, caseType string, in CaseTypeUpsert) (*CaseType, error) {
	if caseType == "" {
		return nil, errors.New("caseType is required")
	}
	if err := validateCaseTypeUpsert(in, false); err != nil {
		return nil, err
	}
	if in.BpmnVersion <= 0 {
		in.BpmnVersion = 1
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE business_operation_types
		SET business_area = $2, operation_name = $3, bpmn_process_id = $4,
		    bpmn_version = $5, workflow_enabled = $6, default_sla_policy_id = NULLIF($7, ''),
		    maker_role = $8, checker_role = $9, owner_service = $10,
		    status = $11, updated_at = CURRENT_TIMESTAMP
		WHERE case_type = $1
		RETURNING case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
		          workflow_enabled, default_sla_policy_id, maker_role, checker_role,
		          owner_service, status, effective_from, effective_to
	`, caseType, in.BusinessArea, in.OperationName, in.BpmnProcessID, in.BpmnVersion,
		in.WorkflowEnabled, in.DefaultSLAPolicyID, in.MakerRole, in.CheckerRole,
		in.OwnerService, statusOrActive(in.Status))
	ct, err := scanCaseType(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ct, err
}

func (r *CaseRepository) GetCaseType(ctx context.Context, caseType string) (*CaseType, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
		       workflow_enabled, default_sla_policy_id, maker_role, checker_role,
		       owner_service, status, effective_from, effective_to
		FROM business_operation_types
		WHERE case_type = $1 AND status = 'ACTIVE'
	`, caseType)
	ct, err := scanCaseType(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ct, nil
}

func (r *CaseRepository) CreateCase(ctx context.Context, in CaseCreate) (*BusinessCase, error) {
	if err := validateCreate(in); err != nil {
		return nil, err
	}
	if in.Priority == "" {
		in.Priority = "NORMAL"
	}
	if in.CaseCode == "" {
		code, err := r.nextCaseCode(ctx, in.CaseType)
		if err != nil {
			return nil, err
		}
		in.CaseCode = code
	}

	ct, err := r.GetCaseType(ctx, in.CaseType)
	if err != nil {
		return nil, err
	}
	if ct == nil {
		return nil, fmt.Errorf("unknown caseType %q", in.CaseType)
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		INSERT INTO business_cases (
			id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
			domain_service, status, priority, created_by, candidate_role, sla_policy_id,
			bpmn_process_id, bpmn_version
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
		          domain_service, status, current_step, priority, created_by, assigned_to,
		          candidate_role, sla_policy_id, sla_due_at, process_instance_key,
		          bpmn_process_id, bpmn_version, created_at, updated_at, completed_at
	`, id, in.TenantID, in.CaseType, in.CaseCode, in.Title, in.PrimaryObjectType, in.PrimaryObjectID,
		in.DomainService, CaseStatusDraft, in.Priority, in.CreatedBy, ct.MakerRole, ct.DefaultSLAPolicyID,
		ct.BpmnProcessID, ct.BpmnVersion)

	bc, err := scanBusinessCase(row)
	if err != nil {
		return nil, err
	}
	if err := addTimelineTx(ctx, tx, bc.ID, "CASE_CREATED", nil, ptr(CaseStatusDraft), ptr(in.CreatedBy), ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &bc, nil
}

func (r *CaseRepository) ListCases(ctx context.Context, f CaseListFilter) ([]BusinessCase, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 100
	}

	where := []string{"1=1"}
	args := []any{}
	add := func(sql string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(sql, len(args)))
	}
	if f.CaseType != "" {
		add("case_type = $%d", f.CaseType)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.AssignedTo != "" {
		add("assigned_to = $%d", f.AssignedTo)
	}
	if f.CandidateRole != "" {
		add("candidate_role = $%d", f.CandidateRole)
	}
	if f.Keyword != "" {
		args = append(args, f.Keyword, f.Keyword, f.Keyword)
		n := len(args)
		where = append(where, fmt.Sprintf("(case_code ILIKE '%%' || $%d || '%%' OR title ILIKE '%%' || $%d || '%%' OR primary_object_id ILIKE '%%' || $%d || '%%')", n-2, n-1, n))
	}

	args = append(args, f.Limit)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
		       domain_service, status, current_step, priority, created_by, assigned_to,
		       candidate_role, sla_policy_id, sla_due_at, process_instance_key,
		       bpmn_process_id, bpmn_version, created_at, updated_at, completed_at
		FROM business_cases
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY updated_at DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BusinessCase
	for rows.Next() {
		bc, err := scanBusinessCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, bc)
	}
	return out, rows.Err()
}

func (r *CaseRepository) GetCase(ctx context.Context, id string) (*BusinessCase, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
		       domain_service, status, current_step, priority, created_by, assigned_to,
		       candidate_role, sla_policy_id, sla_due_at, process_instance_key,
		       bpmn_process_id, bpmn_version, created_at, updated_at, completed_at
		FROM business_cases
		WHERE id = $1
	`, id)
	bc, err := scanBusinessCase(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &bc, nil
}

func (r *CaseRepository) SubmitCase(ctx context.Context, id string, actor string, processInstanceKey int64) (*BusinessCase, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var fromStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM business_cases WHERE id = $1 FOR UPDATE`, id).Scan(&fromStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if fromStatus != CaseStatusDraft {
		return nil, fmt.Errorf("case status must be %s, got %s", CaseStatusDraft, fromStatus)
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE business_cases
		SET status = $2, current_step = 'submitted', process_instance_key = $3,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
		          domain_service, status, current_step, priority, created_by, assigned_to,
		          candidate_role, sla_policy_id, sla_due_at, process_instance_key,
		          bpmn_process_id, bpmn_version, created_at, updated_at, completed_at
	`, id, CaseStatusSubmitted, processInstanceKey)
	bc, err := scanBusinessCase(row)
	if err != nil {
		return nil, err
	}
	if err := addTimelineTx(ctx, tx, id, "CASE_SUBMITTED", &fromStatus, ptr(CaseStatusSubmitted), ptr(actor), ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &bc, nil
}

func (r *CaseRepository) ClaimCase(ctx context.Context, id string, actor string) (*BusinessCase, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		UPDATE business_cases
		SET status = CASE WHEN status = $2 THEN $3 ELSE status END,
		    assigned_to = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
		          domain_service, status, current_step, priority, created_by, assigned_to,
		          candidate_role, sla_policy_id, sla_due_at, process_instance_key,
		          bpmn_process_id, bpmn_version, created_at, updated_at, completed_at
	`, id, CaseStatusSubmitted, CaseStatusInReview, actor)
	bc, err := scanBusinessCase(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := addTimelineTx(ctx, tx, id, "CASE_CLAIMED", nil, ptr(bc.Status), ptr(actor), ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &bc, nil
}

func (r *CaseRepository) MarkCaseAtStep(ctx context.Context, processInstanceKey int64, stepID string, candidateRole string) error {
	if processInstanceKey == 0 || stepID == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE business_cases
		SET status = CASE WHEN status = $2 THEN $3 ELSE status END,
		    current_step = $4,
		    candidate_role = NULLIF($5, ''),
		    updated_at = CURRENT_TIMESTAMP
		WHERE process_instance_key = $1
	`, processInstanceKey, CaseStatusSubmitted, CaseStatusInReview, stepID, candidateRole)
	return err
}

func (r *CaseRepository) MarkCaseStepCompleted(ctx context.Context, processInstanceKey int64, stepID string) error {
	if processInstanceKey == 0 || stepID == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE business_cases
		SET current_step = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE process_instance_key = $1 AND current_step = $2
	`, processInstanceKey, stepID)
	return err
}

func (r *CaseRepository) FinishCase(ctx context.Context, processInstanceKey int64, finalStatus string) error {
	if processInstanceKey == 0 {
		return nil
	}
	if finalStatus == "" {
		finalStatus = CaseStatusCompleted
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE business_cases
		SET status = $2, completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE process_instance_key = $1 AND completed_at IS NULL
	`, processInstanceKey, finalStatus)
	return err
}

func (r *CaseRepository) GetCaseByProcessInstanceKey(ctx context.Context, processInstanceKey int64) (*BusinessCase, error) {
	if processInstanceKey == 0 {
		return nil, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
		       domain_service, status, current_step, priority, created_by, assigned_to,
		       candidate_role, sla_policy_id, sla_due_at, process_instance_key,
		       bpmn_process_id, bpmn_version, created_at, updated_at, completed_at
		FROM business_cases
		WHERE process_instance_key = $1
	`, processInstanceKey)
	bc, err := scanBusinessCase(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &bc, nil
}

func (r *CaseRepository) AddTimelineEvent(ctx context.Context, caseID, eventType, note string) error {
	if caseID == "" || eventType == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO case_timeline_events (case_id, event_type, note)
		VALUES ($1, $2, $3)
	`, caseID, eventType, note)
	return err
}

func (r *CaseRepository) ListTimeline(ctx context.Context, caseID string) ([]TimelineEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, case_id, event_type, from_status, to_status, actor, note, data::text, created_at
		FROM case_timeline_events
		WHERE case_id = $1
		ORDER BY created_at ASC, id ASC
	`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TimelineEvent
	for rows.Next() {
		ev, err := scanTimelineEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ListActiveCasesWithProcess returns in-flight cases that have a Zeebe process instance.
func (r *CaseRepository) ListActiveCasesWithProcess(ctx context.Context, limit int) ([]BusinessCase, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
		       domain_service, status, current_step, priority, created_by, assigned_to,
		       candidate_role, sla_policy_id, sla_due_at, process_instance_key,
		       bpmn_process_id, bpmn_version, created_at, updated_at, completed_at
		FROM business_cases
		WHERE process_instance_key IS NOT NULL
		  AND completed_at IS NULL
		  AND status IN ($1, $2)
		ORDER BY updated_at DESC
		LIMIT $3
	`, CaseStatusSubmitted, CaseStatusInReview, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BusinessCase
	for rows.Next() {
		bc, err := scanBusinessCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, bc)
	}
	return out, rows.Err()
}

func validateCreate(in CaseCreate) error {
	switch {
	case in.TenantID == "":
		return errors.New("tenantId is required")
	case in.CaseType == "":
		return errors.New("caseType is required")
	case in.Title == "":
		return errors.New("title is required")
	case in.PrimaryObjectType == "":
		return errors.New("primaryObjectType is required")
	case in.PrimaryObjectID == "":
		return errors.New("primaryObjectId is required")
	case in.DomainService == "":
		return errors.New("domainService is required")
	case in.CreatedBy == "":
		return errors.New("createdBy is required")
	default:
		return nil
	}
}

func validateCaseTypeUpsert(in CaseTypeUpsert, requireCode bool) error {
	switch {
	case requireCode && in.CaseType == "":
		return errors.New("caseType is required")
	case in.BusinessArea == "":
		return errors.New("businessArea is required")
	case in.OperationName == "":
		return errors.New("operationName is required")
	case in.BpmnProcessID == "":
		return errors.New("bpmnProcessId is required")
	case in.MakerRole == "":
		return errors.New("makerRole is required")
	case in.CheckerRole == "":
		return errors.New("checkerRole is required")
	case in.OwnerService == "":
		return errors.New("ownerService is required")
	default:
		return nil
	}
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func addTimelineTx(ctx context.Context, tx *sql.Tx, caseID, eventType string, fromStatus, toStatus, actor *string, note string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO case_timeline_events (case_id, event_type, from_status, to_status, actor, note)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, caseID, eventType, fromStatus, toStatus, actor, note)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCaseType(s scanner) (CaseType, error) {
	var ct CaseType
	var defaultSLA sql.NullString
	var effectiveTo sql.NullTime
	err := s.Scan(&ct.CaseType, &ct.BusinessArea, &ct.OperationName, &ct.BpmnProcessID, &ct.BpmnVersion,
		&ct.WorkflowEnabled, &defaultSLA, &ct.MakerRole, &ct.CheckerRole, &ct.OwnerService,
		&ct.Status, &ct.EffectiveFrom, &effectiveTo)
	ct.DefaultSLAPolicyID = nullStringPtr(defaultSLA)
	if effectiveTo.Valid {
		ct.EffectiveTo = &effectiveTo.Time
	}
	return ct, err
}

func scanBusinessCase(s scanner) (BusinessCase, error) {
	var bc BusinessCase
	var assignedTo, candidateRole, slaPolicyID, bpmnProcessID sql.NullString
	var slaDueAt, completedAt sql.NullTime
	var processInstanceKey sql.NullInt64
	var bpmnVersion sql.NullInt64
	err := s.Scan(&bc.ID, &bc.TenantID, &bc.CaseType, &bc.CaseCode, &bc.Title,
		&bc.PrimaryObjectType, &bc.PrimaryObjectID, &bc.DomainService, &bc.Status,
		&bc.CurrentStep, &bc.Priority, &bc.CreatedBy, &assignedTo, &candidateRole,
		&slaPolicyID, &slaDueAt, &processInstanceKey, &bpmnProcessID, &bpmnVersion,
		&bc.CreatedAt, &bc.UpdatedAt, &completedAt)
	bc.AssignedTo = nullStringPtr(assignedTo)
	bc.CandidateRole = nullStringPtr(candidateRole)
	bc.SLAPolicyID = nullStringPtr(slaPolicyID)
	if slaDueAt.Valid {
		bc.SLADueAt = &slaDueAt.Time
	}
	if processInstanceKey.Valid {
		bc.ProcessInstanceKey = &processInstanceKey.Int64
	}
	bc.BpmnProcessID = nullStringPtr(bpmnProcessID)
	if bpmnVersion.Valid {
		v := int(bpmnVersion.Int64)
		bc.BpmnVersion = &v
	}
	if completedAt.Valid {
		bc.CompletedAt = &completedAt.Time
	}
	return bc, err
}

func scanTimelineEvent(s scanner) (TimelineEvent, error) {
	var ev TimelineEvent
	var fromStatus, toStatus, actor sql.NullString
	var data string
	err := s.Scan(&ev.ID, &ev.CaseID, &ev.EventType, &fromStatus, &toStatus, &actor, &ev.Note, &data, &ev.CreatedAt)
	ev.FromStatus = nullStringPtr(fromStatus)
	ev.ToStatus = nullStringPtr(toStatus)
	ev.Actor = nullStringPtr(actor)
	ev.Data = json.RawMessage(data)
	return ev, err
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func ptr(s string) *string {
	return &s
}
