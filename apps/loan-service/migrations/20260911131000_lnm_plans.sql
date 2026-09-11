-- +goose Up

-- Loan plan management catalog (W7): kế hoạch vay theo kỳ, dùng làm danh mục
-- tham chiếu cho hồ sơ/hợp đồng.

CREATE TABLE IF NOT EXISTS lnm_plans (
    id                  VARCHAR(64) PRIMARY KEY,
    tenant_id           VARCHAR(64) NOT NULL,
    code                VARCHAR(64) NOT NULL,
    name                VARCHAR(255) NOT NULL,
    from_date           DATE,
    to_date             DATE,
    target_amount_minor BIGINT NOT NULL DEFAULT 0,
    note                TEXT NOT NULL DEFAULT '',
    status              VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    org_code            VARCHAR(64),
    created_by          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    version             INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

-- +goose Down

DROP TABLE IF EXISTS lnm_plans;
