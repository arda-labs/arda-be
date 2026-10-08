package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

const collectionColumns = `id, tenant_id, contract_code, agreement_code, collection_date::text,
	principal_minor, interest_minor, currency_code, status, payload,
	workflow_case_id::text, COALESCE(workflow_case_code,''), journal_entry_id::text, created_by, created_at, updated_at,
	version`

// collectionSortCol maps the public sort key (whitelisted in the handler
// ListSpec) to a SQL column; unknown keys fall back to created_at.
func collectionSortCol(field string) string {
	switch field {
	case "agreement_code":
		return "agreement_code"
	case "contract_code":
		return "contract_code"
	case "collection_date":
		return "collection_date"
	case "created_at":
		return "created_at"
	default:
		return "created_at"
	}
}

func (r *LoanRepository) ListCollections(ctx context.Context, tenantID string, orgCodes []string, status, contractCode, q, sort, order string, limit, offset int) ([]domain.Collection, int, error) {
	orgAny := orgCodesToAny(orgCodes)
	sortCol := collectionSortCol(sort)
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+collectionColumns+`, count(*) OVER() AS total_count
		FROM lnm_collections
		WHERE tenant_id = $1::text
		  AND ($2::text = '' OR status = $2::text)
		  AND ($3::text = '' OR contract_code = $3::text)
		  AND ($4::text = '' OR agreement_code ILIKE '%' || $4::text || '%' OR contract_code ILIKE '%' || $4::text || '%')
		  AND ($5::text[] IS NULL OR org_code = ANY($5::text[]))
		ORDER BY `+sortCol+` `+cashFlowDirection(sort, order)+`, id
		LIMIT $6::int OFFSET $7::int`,
		tenantID, status, contractCode, q, orgAny, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.Collection{}
	total := 0
	for rows.Next() {
		var c domain.Collection
		var caseID, entryID sql.NullString
		var payload []byte
		if err := rows.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.AgreementCode, &c.CollectionDate,
			&c.PrincipalMinor, &c.InterestMinor, &c.CurrencyCode, &c.Status, &payload,
			&caseID, &c.WorkflowCaseCode, &entryID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.DataVersion, &total); err != nil {
			return nil, 0, err
		}
		if len(payload) > 0 && string(payload) != "null" {
			c.Payload = payload
		}
		if caseID.Valid {
			c.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			c.JournalEntryID = &entryID.String
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *LoanRepository) CreateCollection(ctx context.Context, c *domain.Collection) (*domain.Collection, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_collections
			(tenant_id, contract_code, agreement_code, collection_date, principal_minor,
			 interest_minor, currency_code, status, org_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'DRAFT',$8,$9)
		RETURNING id, created_at, updated_at`,
		c.TenantID, c.ContractCode, c.AgreementCode, c.CollectionDate, c.PrincipalMinor,
		c.InterestMinor, c.CurrencyCode, nullText(c.OrgCode), c.CreatedBy)
	if err := row.Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return c, nil
}

func (r *LoanRepository) GetCollection(ctx context.Context, tenantID, id string) (*domain.Collection, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+collectionColumns+` FROM lnm_collections WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	var c domain.Collection
	var caseID, entryID sql.NullString
	var payload []byte
	err := row.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.AgreementCode, &c.CollectionDate,
		&c.PrincipalMinor, &c.InterestMinor, &c.CurrencyCode, &c.Status, &payload,
		&caseID, &c.WorkflowCaseCode, &entryID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.DataVersion)
	if err == sql.ErrNoRows {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if len(payload) > 0 && string(payload) != "null" {
		c.Payload = payload
	}
	if caseID.Valid {
		c.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		c.JournalEntryID = &entryID.String
	}
	return &c, nil
}

func (r *LoanRepository) SetCollectionStatus(ctx context.Context, tenantID, id, from, to, updatedBy, reason string) error {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.Status(from), domain.Status(to), reason); err != nil {
		return err
	}
	expected := domain.DataVersionFromContext(ctx)
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_collections SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = $5
		  AND ($6 = 0 OR version = $6)`, tenantID, id, to, updatedBy, from, expected)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if staleVersion(ctx, r.db, "lnm_collections", tenantID, id, expected) {
			return ErrStaleVersion
		}
		var current string
		lookupErr := r.db.QueryRowContext(ctx, `SELECT status FROM lnm_collections WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&current)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("%w", ErrNotFound)
		}
		if lookupErr != nil {
			return lookupErr
		}
		return fmt.Errorf("%w: collection is %s, expected %s", domain.ErrInvalidTransition, current, from)
	}
	return nil
}

func (r *LoanRepository) SetCollectionCaseAndJournal(ctx context.Context, tenantID, id, caseID, caseCode, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_collections SET workflow_case_id = $3, workflow_case_code = $4, journal_entry_id = $5, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nullText(caseID), nullText(caseCode), nullText(journalEntryID))
	return err
}

// SettleCollectionTx atomically posts an approved collection and applies its
// agreement side effects. A previously posted row with a journal entry is an
// idempotent replay; any other non-approved state is rejected.
func (r *LoanRepository) SettleCollectionTx(ctx context.Context, tenantID, id, journalEntryID, updatedBy string) (bool, error) {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.StatusApproved, domain.StatusPosted, ""); err != nil {
		return false, err
	}
	if err := domain.CanTransition(domain.AgreementMachine, domain.StatusActive, domain.StatusClosed, ""); err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	expected := domain.DataVersionFromContext(ctx)
	var agreementCode string
	var principalMinor, interestMinor int64
	err = tx.QueryRowContext(ctx, `
		UPDATE lnm_collections
		SET status = 'POSTED', journal_entry_id = $3, updated_by = $4,
		    updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'APPROVED'
		  AND ($5 = 0 OR version = $5)
		RETURNING agreement_code, principal_minor, interest_minor`,
		tenantID, id, nullText(journalEntryID), nullText(updatedBy), expected).
		Scan(&agreementCode, &principalMinor, &interestMinor)
	if errors.Is(err, sql.ErrNoRows) {
		var status string
		var existingJournalID sql.NullString
		var version int64
		lookupErr := tx.QueryRowContext(ctx, `
			SELECT status, journal_entry_id::text, version
			FROM lnm_collections WHERE tenant_id = $1 AND id = $2 FOR UPDATE`,
			tenantID, id).Scan(&status, &existingJournalID, &version)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		if lookupErr != nil {
			return false, lookupErr
		}
		if status == domain.CollectionPosted && existingJournalID.Valid {
			if err := tx.Commit(); err != nil {
				return false, err
			}
			return false, nil
		}
		if expected != 0 && version != expected {
			return false, ErrStaleVersion
		}
		return false, ErrCollectionNotApproved
	}
	if err != nil {
		return false, err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE lnm_agreements
		SET outstanding_amt_minor = GREATEST(outstanding_amt_minor - $3, 0),
		    coln_principal_amt_minor = coln_principal_amt_minor + $3,
		    coln_interest_amt_minor = coln_interest_amt_minor + $4,
		    status = CASE WHEN GREATEST(outstanding_amt_minor - $3, 0) = 0 THEN 'CLOSED' ELSE status END,
		    updated_at = now()
		WHERE tenant_id = $1 AND agreement_code = $2`,
		tenantID, agreementCode, principalMinor, interestMinor)
	if err != nil {
		return false, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return false, fmt.Errorf("agreement %s not found while settling collection", agreementCode)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ApplyCollection applies the posting side effect: reduce outstanding
// principal, accumulate collected principal/interest.
func (r *LoanRepository) ApplyCollection(ctx context.Context, tenantID, agreementCode string, principalMinor, interestMinor int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_agreements
		SET outstanding_amt_minor = GREATEST(outstanding_amt_minor - $3, 0),
		    coln_principal_amt_minor = coln_principal_amt_minor + $3,
		    coln_interest_amt_minor = coln_interest_amt_minor + $4,
		    status = CASE WHEN GREATEST(outstanding_amt_minor - $3, 0) = 0 THEN 'CLOSED' ELSE status END,
		    updated_at = now()
		WHERE tenant_id = $1 AND agreement_code = $2`,
		tenantID, agreementCode, principalMinor, interestMinor)
	return err
}
