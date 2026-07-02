package repository

import (
	"context"
	"database/sql"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
)

type ConfigRepository struct {
	db *sql.DB
}

func NewConfigRepository(db *sql.DB) *ConfigRepository {
	return &ConfigRepository{db: db}
}

func (r *ConfigRepository) ListProcessConfigs(ctx context.Context, tenantID string) ([]domain.ProcessConfig, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, case_type, business_area, operation_name, bpmn_process_id,
		       bpmn_version, workflow_enabled, default_sla_policy_id, maker_role,
		       checker_role, owner_service, status, effective_from, effective_to
		FROM fin_process_configs
		WHERE tenant_id = $1
		ORDER BY business_area, operation_name
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []domain.ProcessConfig
	for rows.Next() {
		var c domain.ProcessConfig
		var sla sql.NullString
		var effectiveTo sql.NullTime
		if err := rows.Scan(&c.ID, &c.TenantID, &c.CaseType, &c.BusinessArea,
			&c.OperationName, &c.BPMNProcessID, &c.BPMNVersion, &c.WorkflowEnabled,
			&sla, &c.MakerRole, &c.CheckerRole, &c.OwnerService, &c.Status,
			&c.EffectiveFrom, &effectiveTo); err != nil {
			return nil, err
		}
		c.DefaultSLAPolicyID = sla.String
		if effectiveTo.Valid {
			c.EffectiveTo = &effectiveTo.Time
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

func (r *ConfigRepository) ListAccountClassifications(ctx context.Context, tenantID string) ([]domain.AccountClassification, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, txn_type, direction, product_code, channel,
		       org_code, account_code, regulatory_account_code, internal_account_code, status
		FROM fin_account_classifications
		WHERE tenant_id = $1
		ORDER BY direction, txn_type, code
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.AccountClassification
	for rows.Next() {
		var item domain.AccountClassification
		var product, channel, org, reg, internal sql.NullString
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name,
			&item.TxnType, &item.Direction, &product, &channel, &org,
			&item.AccountCode, &reg, &internal, &item.Status); err != nil {
			return nil, err
		}
		item.ProductCode = product.String
		item.Channel = channel.String
		item.OrgCode = org.String
		item.RegulatoryAccountCode = reg.String
		item.InternalAccountCode = internal.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ConfigRepository) ListJournalDefinitions(ctx context.Context, tenantID string) ([]domain.JournalDefinition, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, txn_type, direction, debit_account_code,
		       credit_account_code, amount_source, description_template, status
		FROM fin_journal_definitions
		WHERE tenant_id = $1
		ORDER BY direction, txn_type, code
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.JournalDefinition
	for rows.Next() {
		var item domain.JournalDefinition
		var descriptionTemplate sql.NullString
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name,
			&item.TxnType, &item.Direction, &item.DebitAccountCode,
			&item.CreditAccountCode, &item.AmountSource, &descriptionTemplate,
			&item.Status); err != nil {
			return nil, err
		}
		item.DescriptionTemplate = descriptionTemplate.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ConfigRepository) FindJournalDefinition(ctx context.Context, tenantID, direction, txnType string) (*domain.JournalDefinition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, txn_type, direction, debit_account_code,
		       credit_account_code, amount_source, description_template, status
		FROM fin_journal_definitions
		WHERE tenant_id = $1 AND direction = $2 AND txn_type = $3 AND status = 'ACTIVE'
		ORDER BY code
		LIMIT 1
	`, tenantID, direction, txnType)

	var item domain.JournalDefinition
	var descriptionTemplate sql.NullString
	if err := row.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name,
		&item.TxnType, &item.Direction, &item.DebitAccountCode,
		&item.CreditAccountCode, &item.AmountSource, &descriptionTemplate,
		&item.Status); err != nil {
		return nil, err
	}
	item.DescriptionTemplate = descriptionTemplate.String
	return &item, nil
}

func (r *ConfigRepository) ListRegulatoryAccounts(ctx context.Context, tenantID string) ([]domain.NamedAccountMapping, error) {
	return r.listNamedAccountMappings(ctx, "fin_regulatory_accounts", tenantID)
}

func (r *ConfigRepository) ListInternalAccounts(ctx context.Context, tenantID string) ([]domain.NamedAccountMapping, error) {
	return r.listNamedAccountMappings(ctx, "fin_internal_accounts", tenantID)
}

func (r *ConfigRepository) listNamedAccountMappings(ctx context.Context, table, tenantID string) ([]domain.NamedAccountMapping, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, account_code, purpose, status
		FROM `+table+`
		WHERE tenant_id = $1
		ORDER BY code
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.NamedAccountMapping
	for rows.Next() {
		var item domain.NamedAccountMapping
		var purpose sql.NullString
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name,
			&item.AccountCode, &purpose, &item.Status); err != nil {
			return nil, err
		}
		item.Purpose = purpose.String
		items = append(items, item)
	}
	return items, rows.Err()
}
