-- +goose Up

-- QTDND membership (thành viên / vốn góp cổ phần). EPAS kept this in
-- crm_inf_member; Arda models it in crm-service so a member is a customer with
-- an ownership stake, not a separate identity.
--
-- Why a new domain: 54 of the 923 QCMS indicators (docs/epas-survey/exports/
-- pcf-kpi-catalog.json, topic "Góp vốn cổ phần") read DC_THANH_VIEN, which had
-- no Arda equivalent — capital-service models source-of-funds contracts (CFM),
-- not member equity.
--
-- Lifecycle: member registration/withdrawal and capital contributions are
-- maker-checker cases (CRM_MEMBER_V1); the approved request moves the member's
-- capital columns. Amounts are int64 minor units.

CREATE TABLE IF NOT EXISTS crm_member_products (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL,
    code           VARCHAR(64) NOT NULL,           -- XAC_LAP | BO_SUNG | ...
    name           VARCHAR(255) NOT NULL,
    share_type     VARCHAR(32) NOT NULL DEFAULT 'ESTABLISH', -- ESTABLISH|ADDITIONAL
    par_value_minor BIGINT NOT NULL DEFAULT 0,     -- mệnh giá một cổ phần
    min_amount_minor BIGINT NOT NULL DEFAULT 0,
    max_amount_minor BIGINT NOT NULL DEFAULT 0,    -- 0 = không giới hạn
    currency_code  VARCHAR(3) NOT NULL DEFAULT 'VND',
    is_active      BOOLEAN NOT NULL DEFAULT true,
    created_by     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     TEXT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS crm_members (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           VARCHAR(64) NOT NULL,
    member_code         VARCHAR(64) NOT NULL,
    customer_code       VARCHAR(64) NOT NULL,
    org_code            VARCHAR(64),
    member_book_no      VARCHAR(64),
    member_type_code    VARCHAR(32) NOT NULL DEFAULT 'INDIVIDUAL', -- INDIVIDUAL|HOUSEHOLD|LEGAL
    open_date           DATE NOT NULL,
    estb_capital_minor  BIGINT NOT NULL DEFAULT 0,  -- vốn góp xác lập đã duyệt
    add_capital_minor   BIGINT NOT NULL DEFAULT 0,  -- vốn góp bổ sung đã duyệt
    total_capital_minor BIGINT NOT NULL DEFAULT 0,  -- = estb + add (maintained on write)
    member_status       VARCHAR(16) NOT NULL DEFAULT 'ACTIVE', -- ACTIVE|LEFT
    leave_date          DATE,
    workflow_case_id    UUID,
    created_by          TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          TEXT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    version             INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, member_code),
    UNIQUE (tenant_id, customer_code)
);

CREATE INDEX IF NOT EXISTS idx_crm_members_org ON crm_members (tenant_id, org_code, member_status);
CREATE INDEX IF NOT EXISTS idx_crm_members_type ON crm_members (tenant_id, member_type_code);

-- One capital movement request: register (xác lập tư cách), additional
-- (bổ sung), withdraw (rút). Approved requests move crm_members; pending and
-- rejected ones only exist here, so the reporting fact can expose both the
-- approved stock and the pending pipeline (indicators 10022/10023/10030-10034).
CREATE TABLE IF NOT EXISTS crm_member_requests (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    member_id        UUID NOT NULL REFERENCES crm_members(id),
    request_type     VARCHAR(16) NOT NULL,   -- REGISTER|ADDITIONAL|WITHDRAW
    product_code     VARCHAR(64),
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    currency_code    VARCHAR(3) NOT NULL DEFAULT 'VND',
    effective_date   DATE,
    status           VARCHAR(16) NOT NULL DEFAULT 'DRAFT', -- DRAFT|SUBMITTED|APPROVED|REJECTED
    reason           TEXT NOT NULL DEFAULT '',
    payload          JSONB NOT NULL DEFAULT '{}',
    workflow_case_id UUID,
    submitted_by     TEXT,
    submitted_at     TIMESTAMPTZ,
    decided_by       TEXT,
    decided_at       TIMESTAMPTZ,
    idempotency_key  VARCHAR(128),
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       TEXT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_crm_member_requests_member
    ON crm_member_requests (tenant_id, member_id, status);
CREATE INDEX IF NOT EXISTS idx_crm_member_requests_org_date
    ON crm_member_requests (tenant_id, effective_date, request_type);

CREATE UNIQUE INDEX IF NOT EXISTS uq_crm_member_request_idem
    ON crm_member_requests (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS uq_crm_member_request_idem;
DROP INDEX IF EXISTS idx_crm_member_requests_org_date;
DROP INDEX IF EXISTS idx_crm_member_requests_member;
DROP TABLE IF EXISTS crm_member_requests;
DROP INDEX IF EXISTS idx_crm_members_type;
DROP INDEX IF EXISTS idx_crm_members_org;
DROP TABLE IF EXISTS crm_members;
DROP TABLE IF EXISTS crm_member_products;
