-- +goose Up

-- Interest configuration of a loan product (T9.1 step 1, design option B in
-- reports/full-review-2026-10/design/loan-product-interest-config.md).
--
-- One row is one complete, versioned configuration for
-- (tenant, product, optional org, effective_from). A version is in force from
-- its effective_from until the next effective_from of the same scope, so there
-- is no effective_to and versions cannot overlap by construction.
--
-- Nothing here is guessed: no business column has a DEFAULT, every code column
-- accepts only the codes the interest calculator knows, and the values the
-- business has not confirmed yet (principal grace days, overdue rate) are
-- nullable and stay NULL until someone enters them. The table is created empty.
--
-- Codes that exist at the EPAS source but that the calculator does not support
-- yet (CSTL_01, CM123.002, CM125.002, CM126.002) are storable so an import
-- loses nothing; the resolver rejects them with a coded error when interest is
-- calculated.

CREATE TABLE lnm_product_interest_config (
    id                    UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id             VARCHAR(64) NOT NULL,
    product_code          VARCHAR(32) NOT NULL,
    -- NULL = tenant-wide version, used for orgs that have no version of their
    -- own. It must be entered on purpose: there is no implicit '%' row.
    org_code              VARCHAR(64),
    effective_from        DATE NOT NULL,

    interest_base_code    VARCHAR(16) NOT NULL
        CONSTRAINT chk_lnm_pic_base_code CHECK (interest_base_code IN ('CSTL_01', 'CSTL_02')),
    interest_calc_method  VARCHAR(16) NOT NULL
        CONSTRAINT chk_lnm_pic_calc_method CHECK (interest_calc_method IN ('CM123.001', 'CM123.002')),
    interest_date_method  VARCHAR(16) NOT NULL
        CONSTRAINT chk_lnm_pic_date_method CHECK (interest_date_method IN ('CM124.001', 'CM124.002')),
    interest_start_method VARCHAR(16) NOT NULL
        CONSTRAINT chk_lnm_pic_start_method CHECK (interest_start_method IN ('CM125.001', 'CM125.002')),
    overdue_check_cond    VARCHAR(16) NOT NULL
        CONSTRAINT chk_lnm_pic_overdue_check CHECK (overdue_check_cond IN ('CM126.001', 'CM126.002')),

    -- Rounding step of the interest total (0 = keep the exact amount) and the
    -- CM163 rounding mode: .001 half-up, .002 ceiling, .003 floor.
    round_no              INTEGER NOT NULL
        CONSTRAINT chk_lnm_pic_round_no CHECK (round_no >= 0),
    round_type            VARCHAR(16) NOT NULL
        CONSTRAINT chk_lnm_pic_round_type CHECK (round_type IN ('CM163.001', 'CM163.002', 'CM163.003')),

    -- Not confirmed by the business yet: NULL means "not configured", never 0.
    principal_grace_days  INTEGER
        CONSTRAINT chk_lnm_pic_grace_days CHECK (principal_grace_days >= 0),
    -- CM127.001: overdue rate as a percentage of the in-term rate.
    overdue_rate_type     VARCHAR(16)
        CONSTRAINT chk_lnm_pic_overdue_rate_type CHECK (overdue_rate_type IN ('CM127.001')),
    overdue_rate_value    NUMERIC(9,6)
        CONSTRAINT chk_lnm_pic_overdue_rate_value CHECK (overdue_rate_value >= 0),

    -- EPAS business key '<org>:<product>:<effective date>' for rows imported
    -- from the source, so a re-run of the import cannot duplicate them.
    source_key            TEXT,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by            TEXT,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by            TEXT,
    version               INTEGER NOT NULL DEFAULT 1,

    CONSTRAINT fk_lnm_pic_product FOREIGN KEY (tenant_id, product_code)
        REFERENCES lnm_products (tenant_id, code),
    CONSTRAINT chk_lnm_pic_org_nonblank CHECK (org_code IS NULL OR btrim(org_code) <> ''),
    -- The overdue rate is one setting: its type and value are entered together.
    CONSTRAINT chk_lnm_pic_overdue_rate_pair CHECK ((overdue_rate_type IS NULL) = (overdue_rate_value IS NULL)),
    -- NULLS NOT DISTINCT so two tenant-wide rows with the same date collide too.
    CONSTRAINT uq_lnm_pic_version UNIQUE NULLS NOT DISTINCT (tenant_id, product_code, org_code, effective_from)
);

CREATE UNIQUE INDEX uq_lnm_pic_source ON lnm_product_interest_config (tenant_id, source_key)
    WHERE source_key IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS lnm_product_interest_config;
