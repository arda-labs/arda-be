-- +goose Up

CREATE TABLE IF NOT EXISTS fin_accounts (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    code VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL,       -- ASSET, LIABILITY, EQUITY, INCOME, EXPENSE
    normal_balance VARCHAR(16) NOT NULL,  -- DEBIT or CREDIT
    currency VARCHAR(8) NOT NULL DEFAULT 'VND',
    is_active BOOLEAN NOT NULL DEFAULT true,
    parent_id UUID REFERENCES fin_accounts(id),
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code)
);

CREATE TABLE IF NOT EXISTS fin_transactions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    idempotency_key TEXT UNIQUE,
    txn_type VARCHAR(64) NOT NULL,
    txn_date DATE NOT NULL DEFAULT CURRENT_DATE,
    posted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    description TEXT,
    source_ref TEXT,
    created_by UUID NOT NULL,
    approved_by UUID,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_fin_txn_tenant ON fin_transactions(tenant_id, posted_at DESC);
CREATE INDEX IF NOT EXISTS idx_fin_txn_status ON fin_transactions(status);
CREATE INDEX IF NOT EXISTS idx_fin_txn_idempotency ON fin_transactions(idempotency_key);

CREATE TABLE IF NOT EXISTS fin_ledger_entries (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    entry_id UUID NOT NULL,
    transaction_id UUID NOT NULL REFERENCES fin_transactions(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES fin_accounts(id),
    entry_type VARCHAR(16) NOT NULL,  -- DEBIT or CREDIT
    amount NUMERIC(26,6) NOT NULL,
    currency VARCHAR(8) NOT NULL DEFAULT 'VND',
    posted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    description TEXT,
    metadata JSONB
);

CREATE INDEX IF NOT EXISTS idx_fin_ledger_account ON fin_ledger_entries(account_id, posted_at DESC);
CREATE INDEX IF NOT EXISTS idx_fin_ledger_txn ON fin_ledger_entries(transaction_id);

CREATE TABLE IF NOT EXISTS fin_account_balances (
    account_id UUID PRIMARY KEY REFERENCES fin_accounts(id),
    balance NUMERIC(26,6) NOT NULL DEFAULT 0,
    currency VARCHAR(8) NOT NULL DEFAULT 'VND',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed chart of accounts (simple)
INSERT INTO fin_accounts (id, code, name, type, normal_balance, currency) VALUES
    (uuidv7(), '1000', 'Tiền mặt', 'ASSET', 'DEBIT', 'VND'),
    (uuidv7(), '1100', 'Tiền gửi ngân hàng', 'ASSET', 'DEBIT', 'VND'),
    (uuidv7(), '2000', 'Phải trả người bán', 'LIABILITY', 'CREDIT', 'VND'),
    (uuidv7(), '3000', 'Vốn chủ sở hữu', 'EQUITY', 'CREDIT', 'VND'),
    (uuidv7(), '4000', 'Doanh thu', 'INCOME', 'CREDIT', 'VND'),
    (uuidv7(), '5000', 'Chi phí', 'EXPENSE', 'DEBIT', 'VND');

-- +goose Down
DROP TABLE IF EXISTS fin_account_balances CASCADE;
DROP TABLE IF EXISTS fin_ledger_entries CASCADE;
DROP TABLE IF EXISTS fin_transactions CASCADE;
DROP TABLE IF EXISTS fin_accounts CASCADE;
