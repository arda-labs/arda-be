-- +goose Up

-- Loan products (EPAS lnm_cfg_product + product_dtl): the catalog contracts
-- inherit defaults from. acc_classification links the product to the finance
-- COA class used for its postings (resolved via finance /coa/resolve).

CREATE TABLE lnm_products (
    id                VARCHAR(64) PRIMARY KEY,
    tenant_id         VARCHAR(64) NOT NULL DEFAULT '',
    code              VARCHAR(32) NOT NULL,
    name              VARCHAR(255) NOT NULL,
    product_type      VARCHAR(16) NOT NULL DEFAULT 'TERM',   -- TERM, LIMIT
    currency_code     VARCHAR(8) NOT NULL DEFAULT 'VND',
    interest_rate_code VARCHAR(32),
    interest_rate     NUMERIC(9,6),
    loan_term_from    INTEGER,
    loan_term_to      INTEGER,
    term_unit         VARCHAR(16) NOT NULL DEFAULT 'MONTH',
    min_amount        NUMERIC(20,2),
    max_amount        NUMERIC(20,2),
    acc_classification VARCHAR(64),
    is_active         BOOLEAN NOT NULL DEFAULT TRUE,
    description       TEXT,
    created_by        VARCHAR(64),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code)
);

-- +goose Down
DROP TABLE IF EXISTS lnm_products;
