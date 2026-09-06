-- +goose Up

-- COA v2: four-layer chart-of-accounts inherited from EPAS be_fac design
-- (master-data-catalog §4) — version → chart tree → abstract class →
-- class-to-COA map, plus segment-based account-number structures. The
-- existing fin_accounts table remains the operational account ledger; these
-- tables are the definition layer business flows resolve against.

CREATE TABLE fin_coa_versions (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL DEFAULT 'default',
    code           VARCHAR(64) NOT NULL,
    name           VARCHAR(255) NOT NULL,
    scope          VARCHAR(32) NOT NULL DEFAULT 'GENERAL',  -- GENERAL, BRANCH, PRODUCT
    parent_code    VARCHAR(64),
    effective_date DATE NOT NULL DEFAULT CURRENT_DATE,
    is_default     BOOLEAN NOT NULL DEFAULT FALSE,
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code)
);

CREATE TABLE fin_coa_accounts (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL DEFAULT 'default',
    version_code   VARCHAR(64) NOT NULL,
    acc_code       VARCHAR(64) NOT NULL,
    name           VARCHAR(255) NOT NULL,
    acc_type       VARCHAR(32) NOT NULL,   -- ASSET, LIABILITY, EQUITY, INCOME, EXPENSE
    acc_nature     VARCHAR(16) NOT NULL,   -- DEBIT, CREDIT
    parent_code    VARCHAR(64),
    is_internal    BOOLEAN NOT NULL DEFAULT FALSE,
    is_postable    BOOLEAN NOT NULL DEFAULT TRUE,
    effective_date DATE NOT NULL DEFAULT CURRENT_DATE,
    expiry_date    DATE,
    description    TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, version_code, acc_code),
    FOREIGN KEY (tenant_id, version_code) REFERENCES fin_coa_versions(tenant_id, code)
);
CREATE INDEX fin_coa_accounts_version_idx ON fin_coa_accounts (tenant_id, version_code, parent_code);

CREATE TABLE fin_acc_class_coa_maps (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL DEFAULT 'default',
    classification VARCHAR(64) NOT NULL,    -- fin_account_classifications.code
    coa_version    VARCHAR(64) NOT NULL,
    coa_acc_code   VARCHAR(64) NOT NULL,
    debt_group_code VARCHAR(32) NOT NULL DEFAULT '',
    currency_code  VARCHAR(8) NOT NULL DEFAULT '',
    effective_date DATE NOT NULL DEFAULT CURRENT_DATE,
    expiry_date    DATE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code)
);

CREATE TABLE fin_acc_structures (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    VARCHAR(64) NOT NULL DEFAULT 'default',
    code         VARCHAR(64) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    acc_type     VARCHAR(32) NOT NULL,
    total_length INTEGER NOT NULL DEFAULT 8,
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code)
);

CREATE TABLE fin_acc_structure_segments (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    VARCHAR(64) NOT NULL DEFAULT 'default',
    structure_id UUID NOT NULL REFERENCES fin_acc_structures(id) ON DELETE CASCADE,
    seq_no       INTEGER NOT NULL,
    name         VARCHAR(100) NOT NULL,
    length       INTEGER NOT NULL,
    source       VARCHAR(32) NOT NULL DEFAULT 'FIXED',  -- FIXED, ORG, CURRENCY, PRODUCT, TERM, SEQUENCE
    fixed_value  VARCHAR(64) NOT NULL DEFAULT '',
    is_required  BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE(tenant_id, structure_id, seq_no)
);

-- +goose Down
DROP TABLE IF EXISTS fin_acc_structure_segments;
DROP TABLE IF EXISTS fin_acc_structures;
DROP TABLE IF EXISTS fin_acc_class_coa_maps;
DROP TABLE IF EXISTS fin_coa_accounts;
DROP TABLE IF EXISTS fin_coa_versions;
