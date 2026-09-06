-- +goose Up

-- mdm-service: business master data catalogs. Shape is uniform per catalog:
-- common columns + a validated `attributes` jsonb for domain-specific fields
-- (risk ratios, limits, provisioning). tenant_id IS NULL rows are global
-- reference data seeded here and immutable through the runtime API.

CREATE TABLE mdm_currencies (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64),
    code        VARCHAR(32) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    attributes  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mdm_currencies_code_uk ON mdm_currencies (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_countries (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64),
    code        VARCHAR(32) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    attributes  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mdm_countries_code_uk ON mdm_countries (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_id_document_types (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64),
    code        VARCHAR(32) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    attributes  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mdm_id_document_types_code_uk ON mdm_id_document_types (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_collateral_types (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64),
    code        VARCHAR(32) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    attributes  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mdm_collateral_types_code_uk ON mdm_collateral_types (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_loan_purposes (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64),
    code        VARCHAR(32) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    attributes  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mdm_loan_purposes_code_uk ON mdm_loan_purposes (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_fee_types (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64),
    code        VARCHAR(32) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    attributes  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mdm_fee_types_code_uk ON mdm_fee_types (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_debt_groups (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64),
    code        VARCHAR(32) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    attributes  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mdm_debt_groups_code_uk ON mdm_debt_groups (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_interest_rates (
    id            VARCHAR(64) PRIMARY KEY,
    tenant_id     VARCHAR(64),
    code          VARCHAR(32) NOT NULL,
    name          VARCHAR(255) NOT NULL,
    rate_type     VARCHAR(16) NOT NULL CHECK (rate_type IN ('central', 'loan', 'deposit')),
    apply_type    VARCHAR(16) NOT NULL CHECK (apply_type IN ('by_balance', 'by_term', 'negotiated')),
    currency_code VARCHAR(8),
    description   TEXT,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mdm_interest_rates_code_uk ON mdm_interest_rates (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_interest_rate_tiers (
    id             VARCHAR(64) PRIMARY KEY,
    rate_id        VARCHAR(64) NOT NULL REFERENCES mdm_interest_rates (id),
    effective_from DATE NOT NULL,
    effective_to   DATE,
    amount_from    NUMERIC(20, 2),
    amount_to      NUMERIC(20, 2),
    rate_value     NUMERIC(9, 6) NOT NULL,
    min_rate       NUMERIC(9, 6),
    max_rate       NUMERIC(9, 6),
    decision_no    VARCHAR(128),
    decision_date  DATE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX mdm_interest_rate_tiers_rate_idx ON mdm_interest_rate_tiers (rate_id, effective_from DESC);

-- Seeds: global reference data (tenant_id NULL).
INSERT INTO mdm_currencies (id, code, name, attributes) VALUES
    ('cur_seed_vnd', 'VND', 'Vietnamese Dong', '{"symbol":"₫","decimal_places":0}'),
    ('cur_seed_usd', 'USD', 'US Dollar', '{"symbol":"$","decimal_places":2}'),
    ('cur_seed_eur', 'EUR', 'Euro', '{"symbol":"€","decimal_places":2}'),
    ('cur_seed_gbp', 'GBP', 'Pound Sterling', '{"symbol":"£","decimal_places":2}'),
    ('cur_seed_jpy', 'JPY', 'Japanese Yen', '{"symbol":"¥","decimal_places":0}'),
    ('cur_seed_cny', 'CNY', 'Chinese Yuan', '{"symbol":"¥","decimal_places":2}'),
    ('cur_seed_aud', 'AUD', 'Australian Dollar', '{"symbol":"A$","decimal_places":2}'),
    ('cur_seed_sgd', 'SGD', 'Singapore Dollar', '{"symbol":"S$","decimal_places":2}'),
    ('cur_seed_thb', 'THB', 'Thai Baht', '{"symbol":"฿","decimal_places":2}'),
    ('cur_seed_krw', 'KRW', 'South Korean Won', '{"symbol":"₩","decimal_places":0}'),
    ('cur_seed_chf', 'CHF', 'Swiss Franc', '{"symbol":"Fr","decimal_places":2}');

INSERT INTO mdm_countries (id, code, name, attributes) VALUES
    ('ctry_seed_vn', 'VN', 'Việt Nam', '{"nationality":"Việt Nam"}'),
    ('ctry_seed_us', 'US', 'United States', '{"nationality":"American"}'),
    ('ctry_seed_gb', 'GB', 'United Kingdom', '{"nationality":"British"}'),
    ('ctry_seed_de', 'DE', 'Germany', '{"nationality":"German"}'),
    ('ctry_seed_fr', 'FR', 'France', '{"nationality":"French"}'),
    ('ctry_seed_jp', 'JP', 'Japan', '{"nationality":"Japanese"}'),
    ('ctry_seed_cn', 'CN', 'China', '{"nationality":"Chinese"}'),
    ('ctry_seed_kr', 'KR', 'South Korea', '{"nationality":"Korean"}'),
    ('ctry_seed_sg', 'SG', 'Singapore', '{"nationality":"Singaporean"}'),
    ('ctry_seed_th', 'TH', 'Thailand', '{"nationality":"Thai"}'),
    ('ctry_seed_au', 'AU', 'Australia', '{"nationality":"Australian"}'),
    ('ctry_seed_ch', 'CH', 'Switzerland', '{"nationality":"Swiss"}');

INSERT INTO mdm_id_document_types (id, code, name, description) VALUES
    ('iddoc_seed_cccd', 'CCCD', 'Căn cước công dân', 'Thẻ căn cước công dân theo Luật Căn cước 2023'),
    ('iddoc_seed_cmnd', 'CMND', 'Chứng minh nhân dân', 'Giấy chứng minh nhân dân 12 số (còn hiệu lực chuyển đổi)'),
    ('iddoc_seed_passport', 'PASSPORT', 'Hộ chiếu', 'Hộ chiếu cá nhân còn hiệu lực'),
    ('iddoc_seed_other', 'OTHER', 'Giấy tờ tùy thân khác', 'Các giấy tờ tùy thân khác theo quy định');

-- Debt classification per Thông tư 02/2023/TT-NHNN (căn cứ phân loại nợ).
INSERT INTO mdm_debt_groups (id, code, name, description, attributes) VALUES
    ('debt_seed_1', 'GROUP_1', 'Nhóm 1 - Nợ hiện hành', 'Nợ trong hạn và nợ quá hạn dưới 10 ngày',
     '{"provisioning_percent":0,"allows_restructure":false,"sort_order":1}'),
    ('debt_seed_2', 'GROUP_2', 'Nhóm 2 - Nợ cần chú ý', 'Nợ quá hạn từ 10 ngày đến 30 ngày; gia hạn lần đầu',
     '{"provisioning_percent":5,"allows_restructure":true,"sort_order":2}'),
    ('debt_seed_3', 'GROUP_3', 'Nhóm 3 - Nợ dưới tiêu chuẩn', 'Nợ quá hạn 31-90 ngày; gia hạn lần hai; miễn giảm lãi',
     '{"provisioning_percent":20,"allows_restructure":true,"sort_order":3}'),
    ('debt_seed_4', 'GROUP_4', 'Nhóm 4 - Nợ nghi ngờ', 'Nợ quá hạn 91-180 ngày; gia hạn lần ba; cơ cấu giữ nguyên nợ',
     '{"provisioning_percent":50,"allows_restructure":true,"sort_order":4}'),
    ('debt_seed_5', 'GROUP_5', 'Nhóm 5 - Nợ có khả năng mất vốn', 'Nợ quá hạn trên 180 ngày; chờ xử lý tài sản bảo đảm',
     '{"provisioning_percent":100,"allows_restructure":false,"sort_order":5}');

-- +goose Down

DROP TABLE mdm_interest_rate_tiers;
DROP TABLE mdm_interest_rates;
DROP TABLE mdm_debt_groups;
DROP TABLE mdm_fee_types;
DROP TABLE mdm_loan_purposes;
DROP TABLE mdm_collateral_types;
DROP TABLE mdm_id_document_types;
DROP TABLE mdm_countries;
DROP TABLE mdm_currencies;
