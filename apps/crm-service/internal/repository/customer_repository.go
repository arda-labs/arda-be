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

	"github.com/lib/pq"
)

type Customer struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenantId"`
	OrgID          string         `json:"orgId"`
	CustomerCode   string         `json:"customerCode"`
	WorkflowCaseID string         `json:"workflowCaseId,omitempty"`
	CustomerType   string         `json:"customerType"`
	Name           string         `json:"name"`
	Email          string         `json:"email"`
	Status         string         `json:"status"`
	Mobile         string         `json:"mobile"`
	IdentityNo     string         `json:"identityNo"`
	Address        string         `json:"address"`
	Segment        string         `json:"segment"`
	Rank           string         `json:"rank"`
	RiskLevel      string         `json:"riskLevel"`
	GeneralInfo    map[string]any `json:"generalInfo"`
	PersonalInfo   map[string]any `json:"personalInfo"`
	BusinessInfo   map[string]any `json:"businessInfo"`
	ExtendedInfo   map[string]any `json:"extendedInfo"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type CustomerUpsert struct {
	ID           string         `json:"id,omitempty"`
	TenantID     string         `json:"-"`
	OrgID        string         `json:"-"`
	CustomerType string         `json:"customerType"`
	Name         string         `json:"name"`
	Email        string         `json:"email"`
	Status       string         `json:"status"`
	Mobile       string         `json:"mobile"`
	IdentityNo   string         `json:"identityNo"`
	Address      string         `json:"address"`
	Segment      string         `json:"segment"`
	Rank         string         `json:"rank"`
	RiskLevel    string         `json:"riskLevel"`
	GeneralInfo  map[string]any `json:"generalInfo"`
	PersonalInfo map[string]any `json:"personalInfo"`
	BusinessInfo map[string]any `json:"businessInfo"`
	ExtendedInfo map[string]any `json:"extendedInfo"`
}

type CustomerListFilter struct {
	TenantID     string
	OrgIDs       []string
	CustomerType string
	Status       string
	RiskOnly     bool
	Q            string
	Limit        int
}

type CustomerRelationship struct {
	ID                     string    `json:"id"`
	CustomerID             string    `json:"customerId"`
	RelatedCustomerID      string    `json:"relatedCustomerId"`
	RelatedCustomerCode    string    `json:"relatedCustomerCode"`
	RelatedCustomerName    string    `json:"relatedCustomerName"`
	RelatedCustomerAddress string    `json:"relatedCustomerAddress"`
	RelationType           string    `json:"relationType"`
	RelationCode           string    `json:"relationCode"`
	ReciprocalRelationCode string    `json:"reciprocalRelationCode"`
	Status                 string    `json:"status"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type CustomerRelationshipCreate struct {
	RelatedCustomerID      string `json:"relatedCustomerId"`
	RelationType           string `json:"relationType"`
	RelationCode           string `json:"relationCode"`
	ReciprocalRelationCode string `json:"reciprocalRelationCode"`
	Status                 string `json:"status"`
}

type CustomerRepository struct {
	db *sql.DB
}

func NewCustomerRepository(db *sql.DB) *CustomerRepository {
	return &CustomerRepository{db: db}
}

func (r *CustomerRepository) Create(ctx context.Context, id, name, email, status string) error {
	_, err := r.UpsertCustomer(ctx, CustomerUpsert{
		ID:           id,
		CustomerType: "PERSONAL",
		Name:         name,
		Email:        email,
		Status:       status,
	})
	return err
}

func (r *CustomerRepository) Get(ctx context.Context, id string) (*Customer, error) {
	row := r.db.QueryRowContext(ctx, customerSelect()+` WHERE id = $1`, id)
	return scanCustomer(row)
}

func (r *CustomerRepository) ListCustomers(ctx context.Context, f CustomerListFilter) ([]Customer, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 100
	}

	where := []string{"1=1"}
	args := []any{}
	add := func(clause string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.TenantID != "" {
		add("tenant_id = $%d", f.TenantID)
	}
	if len(f.OrgIDs) > 0 {
		args = append(args, pq.Array(f.OrgIDs))
		n := len(args)
		where = append(where, fmt.Sprintf("(org_id = ANY($%d) OR org_id = '')", n))
	}
	if f.CustomerType != "" {
		add("customer_type = $%d", f.CustomerType)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.RiskOnly {
		where = append(where, "risk_level <> ''")
	}
	if f.Q != "" {
		args = append(args, f.Q, f.Q, f.Q, f.Q, f.Q)
		n := len(args)
		where = append(where, fmt.Sprintf(`(
			customer_code ILIKE '%%' || $%d || '%%'
			OR name ILIKE '%%' || $%d || '%%'
			OR mobile ILIKE '%%' || $%d || '%%'
			OR identity_no ILIKE '%%' || $%d || '%%'
			OR id ILIKE '%%' || $%d || '%%'
		)`, n-4, n-3, n-2, n-1, n))
	}

	args = append(args, f.Limit)
	rows, err := r.db.QueryContext(ctx, customerSelect()+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY updated_at DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Customer{}
	for rows.Next() {
		item, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *CustomerRepository) UpsertCustomer(ctx context.Context, in CustomerUpsert) (*Customer, error) {
	in.ID = strings.TrimSpace(in.ID)
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)
	if in.Name == "" {
		return nil, errors.New("name is required")
	}
	if in.Status == "" {
		in.Status = "DRAFT"
	}
	if in.CustomerType == "" {
		in.CustomerType = "PERSONAL"
	}
	if in.TenantID == "" {
		in.TenantID = "default"
	}

	var existing *Customer
	if in.ID != "" {
		var err error
		existing, err := r.Get(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, errors.New("customer not found")
		}
		if existing.Status != "DRAFT" && existing.Status != "NEEDS_CHANGES" {
			return nil, errors.New("customer cannot be edited in current status")
		}
	}

	customerCode := ""
	if existing != nil {
		customerCode = existing.CustomerCode
		in.TenantID = existing.TenantID
		in.OrgID = existing.OrgID
	} else {
		id, err := newUUID()
		if err != nil {
			return nil, err
		}
		in.ID = id
		customerCode, err = r.nextTempCustomerCode(ctx, in.CustomerType)
		if err != nil {
			return nil, err
		}
		in.OrgID = strings.TrimSpace(in.OrgID)
	}

	general, err := marshalMap(in.GeneralInfo)
	if err != nil {
		return nil, err
	}
	personal, err := marshalMap(in.PersonalInfo)
	if err != nil {
		return nil, err
	}
	business, err := marshalMap(in.BusinessInfo)
	if err != nil {
		return nil, err
	}
	extended, err := marshalMap(in.ExtendedInfo)
	if err != nil {
		return nil, err
	}

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO customers (
			id, tenant_id, org_id, customer_code, customer_type, name, email, status, mobile, identity_no, address,
			segment, customer_rank, risk_level, general_info, personal_info,
			business_info, extended_info, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			customer_type = EXCLUDED.customer_type,
			name = EXCLUDED.name,
			email = EXCLUDED.email,
			status = EXCLUDED.status,
			mobile = EXCLUDED.mobile,
			identity_no = EXCLUDED.identity_no,
			address = EXCLUDED.address,
			segment = EXCLUDED.segment,
			customer_rank = EXCLUDED.customer_rank,
			risk_level = EXCLUDED.risk_level,
			general_info = EXCLUDED.general_info,
			personal_info = EXCLUDED.personal_info,
			business_info = EXCLUDED.business_info,
			extended_info = EXCLUDED.extended_info,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, tenant_id, org_id, customer_code, COALESCE(workflow_case_id, ''), customer_type, name, email, status, mobile, identity_no, address,
		          segment, customer_rank, risk_level, general_info, personal_info,
		          business_info, extended_info, created_at, updated_at
	`, in.ID, in.TenantID, in.OrgID, customerCode, in.CustomerType, in.Name, in.Email, in.Status, in.Mobile, in.IdentityNo, in.Address,
		in.Segment, in.Rank, in.RiskLevel, general, personal, business, extended)
	return scanCustomer(row)
}

func (r *CustomerRepository) AttachWorkflowCase(ctx context.Context, id, workflowCaseID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE customers
		SET workflow_case_id = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, id, workflowCaseID)
	return err
}

func (r *CustomerRepository) AssignOfficialCustomerCode(ctx context.Context, id string) error {
	item, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	if item == nil {
		return errors.New("customer not found")
	}
	if item.CustomerCode != "" &&
		!strings.HasPrefix(item.CustomerCode, "DKKH-T-") &&
		!strings.HasPrefix(item.CustomerCode, "DKKH-O-") {
		return nil
	}
	if item.CustomerCode != "" {
		inUse, err := r.customerCodeUsedByOther(ctx, item.CustomerCode, id)
		if err != nil {
			return err
		}
		if !inUse {
			return nil
		}
	}
	for attempt := 0; attempt < 10; attempt++ {
		code, err := r.nextOfficialCustomerCode(ctx, item.CustomerType)
		if err != nil {
			return err
		}
		if code == item.CustomerCode {
			return nil
		}
		_, err = r.db.ExecContext(ctx, `
			UPDATE customers
			SET customer_code = $2, updated_at = CURRENT_TIMESTAMP
			WHERE id = $1
		`, id, code)
		if err == nil {
			return nil
		}
		if isPgUniqueViolation(err) {
			continue
		}
		return err
	}
	return errors.New("could not assign unique official customer code")
}

func (r *CustomerRepository) customerCodeUsedByOther(ctx context.Context, code, id string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM customers WHERE customer_code = $1 AND id <> $2
		)
	`, code, id).Scan(&exists)
	return exists, err
}

func isPgUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func (r *CustomerRepository) CancelDraft(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE customers
		SET status = 'CANCELLED', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status IN ('DRAFT', 'NEEDS_CHANGES')
	`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("customer cannot be cancelled in current status")
	}
	return nil
}

func (r *CustomerRepository) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE customers
		SET status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, id, status)
	return err
}

func (r *CustomerRepository) Update(ctx context.Context, id, name, email string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE customers
		SET name = $2, email = $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, id, name, email)
	return err
}

func (r *CustomerRepository) HasDuplicateIdentity(ctx context.Context, customerID string) (bool, error) {
	item, err := r.Get(ctx, customerID)
	if err != nil || item == nil {
		return false, err
	}
	taxCode, _ := item.GeneralInfo["taxCode"].(string)
	row := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM customers
			WHERE id <> $1
			  AND tenant_id = $2
			  AND ($3 = '' OR org_id = $3)
			  AND (
			    ($4 <> '' AND identity_no = $4)
			    OR ($5 <> '' AND email = $5)
			    OR ($6 <> '' AND mobile = $6)
			    OR ($7 <> '' AND general_info->>'taxCode' = $7)
			  )
		)
	`, customerID, item.TenantID, item.OrgID, item.IdentityNo, item.Email, item.Mobile, taxCode)
	var exists bool
	return exists, row.Scan(&exists)
}

func (r *CustomerRepository) ListRelationships(ctx context.Context, customerID string) ([]CustomerRelationship, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT rel.id, rel.customer_id, rel.related_customer_id, COALESCE(c.customer_code, c.id, ''),
		       COALESCE(c.name, ''),
		       COALESCE(c.address, ''), rel.relation_type, rel.relation_code,
		       rel.reciprocal_relation_code, rel.status, rel.created_at, rel.updated_at
		FROM customer_relationships rel
		LEFT JOIN customers c ON c.id = rel.related_customer_id
		WHERE rel.customer_id = $1
		ORDER BY rel.created_at DESC
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []CustomerRelationship{}
	for rows.Next() {
		var item CustomerRelationship
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.RelatedCustomerID, &item.RelatedCustomerCode,
			&item.RelatedCustomerName, &item.RelatedCustomerAddress, &item.RelationType, &item.RelationCode,
			&item.ReciprocalRelationCode, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *CustomerRepository) CreateRelationship(ctx context.Context, customerID string, in CustomerRelationshipCreate) (*CustomerRelationship, error) {
	if customerID == "" {
		return nil, errors.New("customerId is required")
	}
	if in.RelatedCustomerID == "" || in.RelationType == "" || in.RelationCode == "" || in.ReciprocalRelationCode == "" {
		return nil, errors.New("relatedCustomerId, relationType, relationCode, and reciprocalRelationCode are required")
	}
	if in.Status == "" {
		in.Status = "ACTIVE"
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO customer_relationships (
			id, customer_id, related_customer_id, relation_type, relation_code,
			reciprocal_relation_code, status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING id, customer_id, related_customer_id, relation_type, relation_code,
		          reciprocal_relation_code, status, created_at, updated_at
	`, id, customerID, in.RelatedCustomerID, in.RelationType, in.RelationCode, in.ReciprocalRelationCode, in.Status)

	var item CustomerRelationship
	if err := row.Scan(&item.ID, &item.CustomerID, &item.RelatedCustomerID, &item.RelationType, &item.RelationCode,
		&item.ReciprocalRelationCode, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if related, err := r.Get(ctx, item.RelatedCustomerID); err == nil && related != nil {
		item.RelatedCustomerName = related.Name
		item.RelatedCustomerAddress = related.Address
	}
	return &item, nil
}

func customerSelect() string {
	return `
		SELECT id, tenant_id, org_id, customer_code, COALESCE(workflow_case_id, ''), customer_type, name, email, status, mobile, identity_no, address,
		       segment, customer_rank, risk_level, general_info, personal_info,
		       business_info, extended_info, created_at, updated_at
		FROM customers`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCustomer(row scanner) (*Customer, error) {
	var item Customer
	var general, personal, business, extended []byte
	err := row.Scan(&item.ID, &item.TenantID, &item.OrgID, &item.CustomerCode, &item.WorkflowCaseID, &item.CustomerType, &item.Name, &item.Email, &item.Status,
		&item.Mobile, &item.IdentityNo, &item.Address, &item.Segment, &item.Rank,
		&item.RiskLevel, &general, &personal, &business, &extended, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.GeneralInfo = unmarshalMap(general)
	item.PersonalInfo = unmarshalMap(personal)
	item.BusinessInfo = unmarshalMap(business)
	item.ExtendedInfo = unmarshalMap(extended)
	return &item, nil
}

func marshalMap(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	return json.Marshal(value)
}

func unmarshalMap(value []byte) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(value, &out)
	return out
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b[:])), nil
}
