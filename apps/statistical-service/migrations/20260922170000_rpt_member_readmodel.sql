-- +goose Up

-- Reporting read model for QTDND membership (step 6): the statistical ETL
-- materialises crm_members + crm_member_requests for one business_date so the
-- 54 membership indicators (PCF topic "Góp vốn cổ phần") resolve without
-- statistical-service ever reading the CRM database.
--
-- Grain: tenant x business_date x member. Capital columns are the member's
-- current approved stake; the request pipeline lives in a second fact so the
-- pending indicators (10023/10030-10034) stay separate from the approved stock.

CREATE TABLE IF NOT EXISTS rpt_fact_member_daily (
    tenant_id          VARCHAR(64) NOT NULL,
    business_date      DATE NOT NULL,
    org_code           VARCHAR(64) NOT NULL DEFAULT '',
    member_code        VARCHAR(64) NOT NULL,
    customer_code      VARCHAR(64) NOT NULL DEFAULT '',
    member_type_code   VARCHAR(32) NOT NULL DEFAULT '',
    member_status      VARCHAR(16) NOT NULL DEFAULT '',
    open_date          DATE,
    leave_date         DATE,
    estb_capital_minor  BIGINT NOT NULL DEFAULT 0,
    add_capital_minor   BIGINT NOT NULL DEFAULT 0,
    total_capital_minor BIGINT NOT NULL DEFAULT 0,
    extracted_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, member_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_member_org
    ON rpt_fact_member_daily (tenant_id, business_date, org_code);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_member_type
    ON rpt_fact_member_daily (tenant_id, business_date, member_type_code);

CREATE TABLE IF NOT EXISTS rpt_fact_member_request_daily (
    tenant_id     VARCHAR(64) NOT NULL,
    business_date DATE NOT NULL,
    org_code      VARCHAR(64) NOT NULL DEFAULT '',
    request_key   VARCHAR(128) NOT NULL,   -- member_code + type + date (stable id)
    member_code   VARCHAR(64) NOT NULL,
    request_type  VARCHAR(16) NOT NULL,    -- REGISTER|ADDITIONAL|WITHDRAW
    status        VARCHAR(16) NOT NULL,    -- DRAFT|SUBMITTED|APPROVED|REJECTED
    amount_minor  BIGINT NOT NULL DEFAULT 0,
    request_date  DATE,
    extracted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, request_key)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_member_request_lookup
    ON rpt_fact_member_request_daily (tenant_id, business_date, status, request_type);

-- +goose Down
DROP TABLE IF EXISTS rpt_fact_member_request_daily;
DROP TABLE IF EXISTS rpt_fact_member_daily;
