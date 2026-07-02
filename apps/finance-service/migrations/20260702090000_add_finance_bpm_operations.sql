-- +goose Up

ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS direction VARCHAR(16);
ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS case_type VARCHAR(96);
ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS operation_name TEXT;
ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS amount NUMERIC(26,6);
ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS currency VARCHAR(8) DEFAULT 'VND';
ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS counterparty_name TEXT;
ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS counterparty_account TEXT;
ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS current_step TEXT;
ALTER TABLE fin_transactions ADD COLUMN IF NOT EXISTS priority VARCHAR(16);

CREATE INDEX IF NOT EXISTS idx_fin_txn_direction ON fin_transactions(tenant_id, direction, posted_at DESC);
CREATE INDEX IF NOT EXISTS idx_fin_txn_case_type ON fin_transactions(tenant_id, case_type, posted_at DESC);

CREATE TABLE IF NOT EXISTS fin_process_configs (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    case_type VARCHAR(96) NOT NULL,
    business_area TEXT NOT NULL,
    operation_name TEXT NOT NULL,
    bpmn_process_id TEXT NOT NULL,
    bpmn_version INT NOT NULL DEFAULT 1,
    workflow_enabled BOOLEAN NOT NULL DEFAULT true,
    default_sla_policy_id TEXT,
    maker_role TEXT NOT NULL,
    checker_role TEXT NOT NULL,
    owner_service TEXT NOT NULL DEFAULT 'finance-service',
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    effective_from TIMESTAMPTZ NOT NULL DEFAULT now(),
    effective_to TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, case_type)
);

CREATE TABLE IF NOT EXISTS fin_account_classifications (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    txn_type TEXT NOT NULL,
    direction VARCHAR(16) NOT NULL,
    product_code TEXT,
    channel TEXT,
    org_code TEXT,
    account_code TEXT NOT NULL,
    regulatory_account_code TEXT,
    internal_account_code TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code)
);

CREATE TABLE IF NOT EXISTS fin_journal_definitions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    txn_type TEXT NOT NULL,
    direction VARCHAR(16) NOT NULL,
    debit_account_code TEXT NOT NULL,
    credit_account_code TEXT NOT NULL,
    amount_source TEXT NOT NULL DEFAULT 'TRANSACTION_AMOUNT',
    description_template TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code)
);

CREATE TABLE IF NOT EXISTS fin_regulatory_accounts (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    account_code TEXT NOT NULL,
    purpose TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code)
);

CREATE TABLE IF NOT EXISTS fin_internal_accounts (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    account_code TEXT NOT NULL,
    purpose TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code)
);

INSERT INTO fin_process_configs (
    tenant_id, case_type, business_area, operation_name, bpmn_process_id,
    bpmn_version, workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('default', 'FINANCE_INCOMING_TRANSACTION', 'Ke toan', 'Giao dich den', 'finance-incoming-transaction-v1', 1, true, 'FINANCE_TXN_MAKER', 'FINANCE_TXN_CHECKER', 'finance-service', 'ACTIVE'),
    ('default', 'FINANCE_OUTGOING_TRANSACTION', 'Ke toan', 'Giao dich di', 'finance-outgoing-transaction-v1', 1, true, 'FINANCE_TXN_MAKER', 'FINANCE_TXN_CHECKER', 'finance-service', 'ACTIVE')
ON CONFLICT (tenant_id, case_type) DO NOTHING;

INSERT INTO fin_regulatory_accounts (tenant_id, code, name, account_code, purpose) VALUES
    ('default', 'BANK_CASH', 'Bank cash account', '1100', 'Incoming and outgoing bank settlement')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO fin_internal_accounts (tenant_id, code, name, account_code, purpose) VALUES
    ('default', 'SUSPENSE_PAYABLE', 'Suspense payable account', '2000', 'Pending customer allocation'),
    ('default', 'OPERATING_BANK', 'Operating bank account', '1100', 'Outgoing payment funding')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO fin_account_classifications (
    tenant_id, code, name, txn_type, direction, account_code,
    regulatory_account_code, internal_account_code
) VALUES
    ('default', 'INCOMING_DEFAULT', 'Default incoming transaction classification', 'INCOMING_TRANSFER', 'INCOMING', '1100', 'BANK_CASH', 'SUSPENSE_PAYABLE'),
    ('default', 'OUTGOING_DEFAULT', 'Default outgoing transaction classification', 'OUTGOING_TRANSFER', 'OUTGOING', '2000', 'BANK_CASH', 'OPERATING_BANK')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO fin_journal_definitions (
    tenant_id, code, name, txn_type, direction,
    debit_account_code, credit_account_code, amount_source, description_template
) VALUES
    ('default', 'INCOMING_TRANSFER_DEFAULT', 'Incoming transfer journal preview', 'INCOMING_TRANSFER', 'INCOMING', '1100', '2000', 'TRANSACTION_AMOUNT', 'Incoming transaction journal preview'),
    ('default', 'OUTGOING_TRANSFER_DEFAULT', 'Outgoing transfer journal preview', 'OUTGOING_TRANSFER', 'OUTGOING', '2000', '1100', 'TRANSACTION_AMOUNT', 'Outgoing transaction journal preview')
ON CONFLICT (tenant_id, code) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS fin_internal_accounts CASCADE;
DROP TABLE IF EXISTS fin_regulatory_accounts CASCADE;
DROP TABLE IF EXISTS fin_journal_definitions CASCADE;
DROP TABLE IF EXISTS fin_account_classifications CASCADE;
DROP TABLE IF EXISTS fin_process_configs CASCADE;
DROP INDEX IF EXISTS idx_fin_txn_case_type;
DROP INDEX IF EXISTS idx_fin_txn_direction;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS priority;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS current_step;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS counterparty_account;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS counterparty_name;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS currency;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS amount;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS operation_name;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS case_type;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS direction;
