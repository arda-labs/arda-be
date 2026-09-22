package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/domain"
)

// ErrMemberConflict marks a write that lost to an existing row (member_code or
// customer_code already registered in the tenant).
var ErrMemberConflict = errors.New("member already exists")

// ErrMemberVersionConflict marks an optimistic-concurrency mismatch: the row
// changed after the caller read it.
var ErrMemberVersionConflict = errors.New("member changed while you were editing")

// MemberRepository persists QTDND membership + capital-movement requests.
type MemberRepository struct {
	db *sql.DB
}

func NewMemberRepository(db *sql.DB) *MemberRepository {
	return &MemberRepository{db: db}
}

const memberColumns = `id::text, tenant_id, member_code, customer_code, COALESCE(org_code,''),
	member_book_no, member_type_code, open_date::text, estb_capital_minor, add_capital_minor,
	total_capital_minor, member_status, COALESCE(leave_date::text,''), workflow_case_id::text,
	created_by, created_at, updated_at, version`

func scanMember(s interface{ Scan(...any) error }) (domain.Member, error) {
	var m domain.Member
	var book, leave, caseID sql.NullString
	if err := s.Scan(&m.ID, &m.TenantID, &m.MemberCode, &m.CustomerCode, &m.OrgCode,
		&book, &m.MemberTypeCode, &m.OpenDate, &m.EstbCapitalMinor, &m.AddCapitalMinor,
		&m.TotalCapitalMinor, &m.MemberStatus, &leave, &caseID,
		&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.DataVersion); err != nil {
		return m, err
	}
	m.MemberBookNo = book.String
	m.LeaveDate = leave.String
	if caseID.Valid {
		m.WorkflowCaseID = &caseID.String
	}
	return m, nil
}

// ListMembersParams carries the member list filters.
type ListMembersParams struct {
	TenantID string
	OrgCodes []string
	Status   string
	TypeCode string
	Q        string
	Sort     string
	Order    string
	Limit    int
	Offset   int
}

// memberSortCol whitelists the sortable columns.
func memberSortCol(sort string) string {
	switch sort {
	case "member_code":
		return "member_code"
	case "open_date":
		return "open_date"
	case "total_capital_minor":
		return "total_capital_minor"
	default:
		return "created_at"
	}
}

// ListMembers returns members filtered by scope/status/type/q with paging.
func (r *MemberRepository) ListMembers(ctx context.Context, p ListMembersParams) ([]domain.Member, int, error) {
	where := []string{"tenant_id = $1"}
	args := []any{p.TenantID}
	if len(p.OrgCodes) > 0 {
		args = append(args, p.OrgCodes)
		where = append(where, fmt.Sprintf("org_code = ANY($%d::text[])", len(args)))
	}
	if p.Status != "" {
		args = append(args, p.Status)
		where = append(where, fmt.Sprintf("member_status = $%d::text", len(args)))
	}
	if p.TypeCode != "" {
		args = append(args, p.TypeCode)
		where = append(where, fmt.Sprintf("member_type_code = $%d::text", len(args)))
	}
	if p.Q != "" {
		args = append(args, "%"+p.Q+"%")
		where = append(where, fmt.Sprintf("(member_code ILIKE $%d OR customer_code ILIKE $%d OR member_book_no ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM crm_members WHERE "+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := p.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s FROM crm_members WHERE %s
		ORDER BY %s %s LIMIT $%d OFFSET $%d`,
		memberColumns, clause, memberSortCol(p.Sort), orderDir(p.Order), len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func orderDir(order string) string {
	if strings.EqualFold(order, "asc") {
		return "ASC"
	}
	return "DESC"
}

// GetMember loads one member by id.
func (r *MemberRepository) GetMember(ctx context.Context, tenantID, id string) (*domain.Member, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+memberColumns+` FROM crm_members WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	m, err := scanMember(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetMemberByCode loads one member by business code.
func (r *MemberRepository) GetMemberByCode(ctx context.Context, tenantID, code string) (*domain.Member, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+memberColumns+` FROM crm_members WHERE tenant_id = $1 AND member_code = $2`, tenantID, code)
	m, err := scanMember(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateMember registers a customer as a member. A duplicate member_code or
// customer_code in the tenant is a conflict, not a retry.
func (r *MemberRepository) CreateMember(ctx context.Context, m *domain.Member) (*domain.Member, error) {
	memberCode := m.MemberCode
	if memberCode == "" {
		memberCode = "TV-" + strings.ToUpper(m.CustomerCode)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO crm_members
			(tenant_id, member_code, customer_code, org_code, member_book_no, member_type_code,
			 open_date, estb_capital_minor, add_capital_minor, total_capital_minor, member_status, created_by)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7::date,$8::bigint,$9::bigint,$8::bigint + $9::bigint,$10,$11)
		RETURNING `+memberColumns,
		m.TenantID, memberCode, m.CustomerCode, m.OrgCode, m.MemberBookNo, m.MemberTypeCode,
		m.OpenDate, m.EstbCapitalMinor, m.AddCapitalMinor, m.MemberStatus, m.CreatedBy)
	out, err := scanMember(row)
	if err != nil && isUniqueViolation(err) {
		return nil, ErrMemberConflict
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateMemberProfile updates the descriptive fields (book no, type, status).
func (r *MemberRepository) UpdateMemberProfile(ctx context.Context, tenantID, id, bookNo, typeCode, status, actor string, version int64) (*domain.Member, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE crm_members
		SET member_book_no = NULLIF($3,''), member_type_code = $4, member_status = $5,
		    leave_date = CASE WHEN $5 = 'LEFT' AND leave_date IS NULL THEN CURRENT_DATE ELSE leave_date END,
		    updated_by = $6, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid AND ($7 = 0 OR version = $7)
		RETURNING `+memberColumns,
		tenantID, id, bookNo, typeCode, status, actor, version)
	out, err := scanMember(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMemberVersionConflict
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplyApprovedCapital moves capital on an approved request, in the caller's
// transaction, guarding a withdrawal against the member's available capital.
// It returns the updated member.
func (r *MemberRepository) ApplyApprovedCapital(ctx context.Context, tx *sql.Tx, tenantID, memberID, requestType string, amountMinor int64, actor string) (*domain.Member, error) {
	var estb, add, total int64
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT estb_capital_minor, add_capital_minor, total_capital_minor, member_status
		FROM crm_members WHERE tenant_id = $1 AND id = $2::uuid FOR UPDATE`, tenantID, memberID).
		Scan(&estb, &add, &total, &status); err != nil {
		return nil, err
	}
	if status != "ACTIVE" {
		return nil, fmt.Errorf("member is not active")
	}

	switch requestType {
	case domain.MemberRequestRegister:
		estb += amountMinor
	case domain.MemberRequestAdditional:
		add += amountMinor
	case domain.MemberRequestWithdraw:
		if amountMinor > total {
			return nil, fmt.Errorf("withdrawal %d exceeds available capital %d", amountMinor, total)
		}
		// Withdraw consumes the additional stake first, then the establishing
		// stake — the order EPAS used so the "tư cách thành viên" survives as
		// long as possible.
		remaining := amountMinor
		if add >= remaining {
			add -= remaining
			remaining = 0
		} else {
			remaining -= add
			add = 0
			estb -= remaining
			if estb < 0 {
				estb = 0
			}
		}
	default:
		return nil, fmt.Errorf("unknown request type %q", requestType)
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE crm_members
		SET estb_capital_minor = $3::bigint, add_capital_minor = $4::bigint,
		    total_capital_minor = $3::bigint + $4::bigint,
		    updated_by = $5, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid
		RETURNING `+memberColumns,
		tenantID, memberID, estb, add, actor)
	out, err := scanMember(row)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

const memberRequestColumns = `id::text, tenant_id, member_id::text, request_type, COALESCE(product_code,''),
	amount_minor, currency_code, COALESCE(effective_date::text,''), status, COALESCE(reason,''),
	workflow_case_id::text, COALESCE(submitted_by,''), submitted_at, COALESCE(decided_by,''), decided_at,
	COALESCE(idempotency_key,''), created_by, created_at, updated_at, version`

func scanMemberRequest(s interface{ Scan(...any) error }) (domain.MemberRequest, error) {
	var m domain.MemberRequest
	var caseID, subBy, decBy, idem sql.NullString
	var subAt, decAt sql.NullTime
	if err := s.Scan(&m.ID, &m.TenantID, &m.MemberID, &m.RequestType, &m.ProductCode,
		&m.AmountMinor, &m.CurrencyCode, &m.EffectiveDate, &m.Status, &m.Reason,
		&caseID, &subBy, &subAt, &decBy, &decAt, &idem,
		&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.DataVersion); err != nil {
		return m, err
	}
	if caseID.Valid {
		m.WorkflowCaseID = &caseID.String
	}
	m.SubmittedBy = subBy.String
	m.DecidedBy = decBy.String
	m.IdempotencyKey = idem.String
	if subAt.Valid {
		m.SubmittedAt = &subAt.Time
	}
	if decAt.Valid {
		m.DecidedAt = &decAt.Time
	}
	return m, nil
}

// CreateMemberRequest stages one capital-movement request (DRAFT).
func (r *MemberRepository) CreateMemberRequest(ctx context.Context, req *domain.MemberRequest) (*domain.MemberRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO crm_member_requests
			(tenant_id, member_id, request_type, product_code, amount_minor, currency_code,
			 effective_date, status, reason, idempotency_key, created_by)
		VALUES ($1,$2::uuid,$3,NULLIF($4,''),$5,$6,NULLIF($7,'')::date,$8,$9,NULLIF($10,''),$11)
		RETURNING `+memberRequestColumns,
		req.TenantID, req.MemberID, req.RequestType, req.ProductCode, req.AmountMinor,
		req.CurrencyCode, req.EffectiveDate, req.Status, req.Reason, req.IdempotencyKey, req.CreatedBy)
	out, err := scanMemberRequest(row)
	if err != nil && isUniqueViolation(err) {
		return nil, ErrMemberConflict
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkMemberRequestSubmitted stamps the workflow case and moves DRAFT→SUBMITTED.
func (r *MemberRepository) MarkMemberRequestSubmitted(ctx context.Context, tenantID, id, caseID, actor string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE crm_member_requests
		SET status = 'SUBMITTED', workflow_case_id = NULLIF($3,'')::uuid, submitted_by = $4,
		    submitted_at = now(), updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid AND status = 'DRAFT'`,
		tenantID, id, caseID, actor)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GetMemberRequest loads one request by id.
func (r *MemberRepository) GetMemberRequest(ctx context.Context, tenantID, id string) (*domain.MemberRequest, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+memberRequestColumns+` FROM crm_member_requests WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	req, err := scanMemberRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// ListMemberRequests returns the request pipeline for a member (or the whole
// tenant when memberID is empty).
func (r *MemberRepository) ListMemberRequests(ctx context.Context, tenantID, memberID, status string) ([]domain.MemberRequest, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if memberID != "" {
		args = append(args, memberID)
		where = append(where, fmt.Sprintf("member_id = $%d::uuid", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+memberRequestColumns+` FROM crm_member_requests
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY created_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MemberRequest{}
	for rows.Next() {
		req, err := scanMemberRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

// BeginTx exposes a transaction for the approve path (capital move + status in
// one atomic step).
func (r *MemberRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}

// ResolveMemberRequest moves SUBMITTED→APPROVED|REJECTED. Only the first
// decision wins so a retried callback is a no-op.
func (r *MemberRepository) ResolveMemberRequest(ctx context.Context, tx *sql.Tx, tenantID, id, status, actor string, dataVersion int64) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE crm_member_requests
		SET status = $3, decided_by = $4, decided_at = now(), updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid AND status = 'SUBMITTED'
		  AND ($5 = 0 OR version = $5)`,
		tenantID, id, status, actor, dataVersion)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if dataVersion > 0 {
		var current int64
		if e := tx.QueryRowContext(ctx, `SELECT version FROM crm_member_requests WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id).Scan(&current); e == nil && current != dataVersion {
			return false, ErrMemberVersionConflict
		}
	}
	return false, nil
}

// ListMembersForReporting returns the whole tenant member slice (optionally one
// org) for the statistical ETL — no paging, includes capital columns.
func (r *MemberRepository) ListMembersForReporting(ctx context.Context, tenantID, orgCode string) ([]domain.Member, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+memberColumns+` FROM crm_members
		WHERE tenant_id = $1 AND ($2 = '' OR org_code = $2)
		ORDER BY member_code`, tenantID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemberCapitalRequestRow is one approved/pending capital request as the
// reporting ETL sees it (joined to the member's org).
type MemberCapitalRequestRow struct {
	RequestID   string `json:"request_id"`
	MemberCode  string `json:"member_code"`
	OrgCode     string `json:"org_code"`
	RequestType string `json:"request_type"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
	RequestDate string `json:"request_date"`
}

// ListMemberRequestsForReporting returns every capital request of the tenant
// with its member code + org, for the pipeline indicators (10023/10030-10034).
func (r *MemberRepository) ListMemberRequestsForReporting(ctx context.Context, tenantID string) ([]MemberCapitalRequestRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id::text, m.member_code, COALESCE(m.org_code,''), r.request_type, r.status,
		       r.amount_minor, COALESCE(r.effective_date, r.created_at::date)::text
		FROM crm_member_requests r JOIN crm_members m ON m.id = r.member_id
		WHERE r.tenant_id = $1
		ORDER BY r.created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MemberCapitalRequestRow{}
	for rows.Next() {
		var x MemberCapitalRequestRow
		if err := rows.Scan(&x.RequestID, &x.MemberCode, &x.OrgCode, &x.RequestType, &x.Status,
			&x.AmountMinor, &x.RequestDate); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// isUniqueViolation reports a PostgreSQL unique-constraint error without
// importing pgconn into this file's contract (the customer repo does the same
// through pgconn; this keeps member writes self-contained).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
}
