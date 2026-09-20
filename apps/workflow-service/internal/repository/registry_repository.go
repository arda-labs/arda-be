package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// CaseTypeStep is one human step of a case type at a registry version. The
// registry is the single source of truth for discovery, allowed actions and
// form keys (workflow registry v2, migration 20260919100000).
type CaseTypeStep struct {
	ID                string
	CaseType          string
	RegistryVersion   int
	ElementID         string
	StepCode          string
	StepKind          string
	FormKey           string
	AllowedActions    []string
	RequiredCommentOn []string
	DataContract      string
	SortOrder         int
	Status            string
}

// OperationTypeRef is the case-type → process mapping used by the bootstrap
// to seed the registry from the embedded BPMN corpus.
type OperationTypeRef struct {
	CaseType        string
	BpmnProcessID   string
	RegistryVersion int
	OwnerService    string
	Status          string
}

func (r *CaseRepository) ListActiveOperationTypes(ctx context.Context) ([]OperationTypeRef, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT case_type, bpmn_process_id, registry_version, owner_service, status
		FROM business_operation_types
		WHERE status = 'ACTIVE'
		ORDER BY case_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OperationTypeRef, 0)
	for rows.Next() {
		var ref OperationTypeRef
		if err := rows.Scan(&ref.CaseType, &ref.BpmnProcessID, &ref.RegistryVersion, &ref.OwnerService, &ref.Status); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// UpsertCaseTypeSteps writes the derived step metadata for one case type and
// registry version. Rows that disappear from the derived set are marked
// INACTIVE instead of deleted so in-flight cases keep resolving.
func (r *CaseRepository) UpsertCaseTypeSteps(ctx context.Context, caseType string, version int, steps []CaseTypeStep) error {
	if strings.TrimSpace(caseType) == "" || version <= 0 {
		return errors.New("caseType and registryVersion are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	keep := make([]string, 0, len(steps))
	for _, step := range steps {
		if step.ElementID == "" {
			continue
		}
		keep = append(keep, step.ElementID)
		id, err := newID()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO workflow_case_type_steps (
				id, case_type, registry_version, element_id, step_code, step_kind,
				form_key, allowed_actions, required_comment_on, data_contract,
				sort_order, status
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'ACTIVE')
			ON CONFLICT (case_type, registry_version, element_id) DO UPDATE SET
				step_code = EXCLUDED.step_code,
				step_kind = EXCLUDED.step_kind,
				form_key = EXCLUDED.form_key,
				allowed_actions = EXCLUDED.allowed_actions,
				required_comment_on = EXCLUDED.required_comment_on,
				data_contract = EXCLUDED.data_contract,
				sort_order = EXCLUDED.sort_order,
				status = 'ACTIVE',
				updated_at = CURRENT_TIMESTAMP
		`, id, caseType, version, step.ElementID, step.StepCode, step.StepKind,
			step.FormKey, step.AllowedActions, step.RequiredCommentOn,
			nullableString(step.DataContract), step.SortOrder)
		if err != nil {
			return fmt.Errorf("upsert step %s/%s: %w", caseType, step.ElementID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE workflow_case_type_steps
		SET status = 'INACTIVE', updated_at = CURRENT_TIMESTAMP
		WHERE case_type = $1 AND registry_version = $2
		  AND status = 'ACTIVE'
		  AND element_id <> ALL($3)
	`, caseType, version, keep); err != nil {
		return fmt.Errorf("retire removed steps for %s: %w", caseType, err)
	}
	return tx.Commit()
}

// ListCaseTypeSteps returns the ACTIVE human steps for a case type and
// registry version ordered by their BPMN appearance.
func (r *CaseRepository) ListCaseTypeSteps(ctx context.Context, caseType string, version int) ([]CaseTypeStep, error) {
	if strings.TrimSpace(caseType) == "" || version <= 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, case_type, registry_version, element_id, step_code, step_kind,
		       form_key, COALESCE(to_json(allowed_actions)::text, '[]'),
		       COALESCE(to_json(required_comment_on)::text, '[]'), COALESCE(data_contract, ''),
		       sort_order, status
		FROM workflow_case_type_steps
		WHERE case_type = $1 AND registry_version = $2 AND status = 'ACTIVE'
		ORDER BY sort_order, element_id
	`, caseType, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CaseTypeStep, 0)
	for rows.Next() {
		var step CaseTypeStep
		var allowedJSON, requiredJSON string
		if err := rows.Scan(&step.ID, &step.CaseType, &step.RegistryVersion, &step.ElementID,
			&step.StepCode, &step.StepKind, &step.FormKey, &allowedJSON,
			&requiredJSON, &step.DataContract, &step.SortOrder, &step.Status); err != nil {
			return nil, err
		}
		// The pgx stdlib returns a text[] column as a raw string; decode the
		// JSON projection instead of scanning straight into []string.
		if err := json.Unmarshal([]byte(allowedJSON), &step.AllowedActions); err != nil {
			return nil, fmt.Errorf("decode allowed_actions for %s: %w", step.ElementID, err)
		}
		if err := json.Unmarshal([]byte(requiredJSON), &step.RequiredCommentOn); err != nil {
			return nil, fmt.Errorf("decode required_comment_on for %s: %w", step.ElementID, err)
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

// FirstHumanStep returns the first registry step of a case type, used for the
// non-actionable "processing" eager seed.
func (r *CaseRepository) FirstHumanStep(ctx context.Context, caseType string, version int) (*CaseTypeStep, error) {
	steps, err := r.ListCaseTypeSteps(ctx, caseType, version)
	if err != nil || len(steps) == 0 {
		return nil, err
	}
	return &steps[0], nil
}

// CaseTypeHasRegistry reports whether the case type has ACTIVE registry steps
// at the given version (capability gate for submit).
func (r *CaseRepository) CaseTypeHasRegistry(ctx context.Context, caseType string, version int) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM workflow_case_type_steps
			WHERE case_type = $1 AND registry_version = $2 AND status = 'ACTIVE'
		)
	`, caseType, version).Scan(&exists)
	return exists, err
}

// CaseTypeRegistryVersion returns the current registry version configured for
// a case type (business_operation_types.registry_version).
func (r *CaseRepository) CaseTypeRegistryVersion(ctx context.Context, caseType string) (int, error) {
	var version sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT registry_version FROM business_operation_types WHERE case_type = $1
	`, caseType).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 1, err
	}
	if !version.Valid || version.Int64 <= 0 {
		return 1, nil
	}
	return int(version.Int64), nil
}

// CaseRegistryVersions batch-resolves the registry version pinned to each case
// (used to decorate work items with their step metadata).
func (r *CaseRepository) CaseRegistryVersions(ctx context.Context, caseIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(caseIDs))
	if len(caseIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, registry_version FROM business_cases WHERE id = ANY($1)
	`, caseIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var version sql.NullInt64
		if err := rows.Scan(&id, &version); err != nil {
			return nil, err
		}
		if version.Valid && version.Int64 > 0 {
			out[id] = int(version.Int64)
		}
	}
	return out, rows.Err()
}

func nullableString(value string) sql.NullString {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: trimmed, Valid: true}
}
