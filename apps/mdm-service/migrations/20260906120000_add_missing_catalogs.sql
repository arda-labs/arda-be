-- +goose Up

-- EPAS parity batch 2 (2026-09-06): catalogs still missing vs
-- docs/epas-survey/master-data-catalog.md groups 1-3. Same uniform shape as
-- batch 1; scoring drops EPAS's SQL-expression anti-pattern deliberately.

CREATE TABLE mdm_economic_types (
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
CREATE UNIQUE INDEX mdm_economic_types_code_uk ON mdm_economic_types (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_industries (
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
CREATE UNIQUE INDEX mdm_industries_code_uk ON mdm_industries (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_loan_methods (
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
CREATE UNIQUE INDEX mdm_loan_methods_code_uk ON mdm_loan_methods (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_loan_contract_types (
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
CREATE UNIQUE INDEX mdm_loan_contract_types_code_uk ON mdm_loan_contract_types (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_fund_sources (
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
CREATE UNIQUE INDEX mdm_fund_sources_code_uk ON mdm_fund_sources (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_fund_purposes (
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
CREATE UNIQUE INDEX mdm_fund_purposes_code_uk ON mdm_fund_purposes (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_base_rates (
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
CREATE UNIQUE INDEX mdm_base_rates_code_uk ON mdm_base_rates (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_interest_factors (
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
CREATE UNIQUE INDEX mdm_interest_factors_code_uk ON mdm_interest_factors (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_cash_denominations (
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
CREATE UNIQUE INDEX mdm_cash_denominations_code_uk ON mdm_cash_denominations (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_scoring_types (
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
CREATE UNIQUE INDEX mdm_scoring_types_code_uk ON mdm_scoring_types (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_scoring_indicator_groups (
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
CREATE UNIQUE INDEX mdm_scoring_indicator_groups_code_uk ON mdm_scoring_indicator_groups (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_scoring_indicators (
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
CREATE UNIQUE INDEX mdm_scoring_indicators_code_uk ON mdm_scoring_indicators (code, COALESCE(tenant_id, ''));

CREATE TABLE mdm_scoring_benchmarks (
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
CREATE UNIQUE INDEX mdm_scoring_benchmarks_code_uk ON mdm_scoring_benchmarks (code, COALESCE(tenant_id, ''));

-- Seeds: national/field-standard reference data only; tenant-specific
-- catalogs stay empty for tenants to configure.

INSERT INTO mdm_economic_types (id, code, name) VALUES
    ('ect_seed_soe', 'SOE', 'Kinh tế nhà nước'),
    ('ect_seed_private', 'PRIVATE', 'Kinh tế tư nhân'),
    ('ect_seed_fdi', 'FDI', 'Đầu tư nước ngoài'),
    ('ect_seed_collective', 'COLLECTIVE', 'Kinh tế tập thể'),
    ('ect_seed_household', 'HOUSEHOLD', 'Hộ gia đình cá thể');

INSERT INTO mdm_industries (id, code, name) VALUES
    ('ind_seed_agri', 'AGRI', 'Nông nghiệp, lâm nghiệp, thủy sản'),
    ('ind_seed_construction', 'CONSTRUCTION', 'Xây dựng'),
    ('ind_seed_manufacturing', 'MANUFACTURING', 'Công nghiệp chế biến chế tạo'),
    ('ind_seed_trade', 'TRADE', 'Bán buôn, bán lẻ'),
    ('ind_seed_transport', 'TRANSPORT', 'Vận tải, kho bãi'),
    ('ind_seed_realestate', 'REAL_ESTATE', 'Kinh doanh bất động sản'),
    ('ind_seed_finance', 'FINANCE', 'Tài chính, ngân hàng, bảo hiểm');

INSERT INTO mdm_loan_methods (id, code, name) VALUES
    ('lmt_seed_installment', 'INSTALLMENT', 'Cho vay trả góp'),
    ('lmt_seed_lumpsum', 'LUMPSUM', 'Cho vay trả một lần cuối kỳ'),
    ('lmt_seed_limit', 'LIMIT', 'Cho vay hạn mức'),
    ('lmt_seed_revolving', 'REVOLVING', 'Cho vay tuần hoàn'),
    ('lmt_seed_overdraft', 'OVERDRAFT', 'Cho vay thấu chi');

INSERT INTO mdm_loan_contract_types (id, code, name, attributes) VALUES
    ('lct_seed_secured', 'SECURED', 'Hợp đồng tín dụng có tài sản bảo đảm', '{"is_collateral":true}'),
    ('lct_seed_unsecured', 'UNSECURED', 'Hợp đồng tín dụng không tài sản bảo đảm', '{"is_collateral":false}'),
    ('lct_seed_guaranteed', 'GUARANTEED', 'Hợp đồng tín dụng có bảo lãnh', '{"is_collateral":true}');

INSERT INTO mdm_fund_sources (id, code, name) VALUES
    ('fsc_seed_deposit', 'DEPOSIT', 'Tiền gửi huy động'),
    ('fsc_seed_interbank', 'INTERBANK', 'Vay liên ngân hàng'),
    ('fsc_seed_equity', 'EQUITY', 'Vốn chủ sở hữu'),
    ('fsc_seed_gov', 'GOVERNMENT', 'Vay Chính phủ / tái cấp vốn');

INSERT INTO mdm_fund_purposes (id, code, name) VALUES
    ('fpu_seed_lending', 'LENDING', 'Cấp tín dụng'),
    ('fpu_seed_investment', 'INVESTMENT', 'Đầu tư chứng khoán'),
    ('fpu_seed_liquidity', 'LIQUIDITY', 'Dự thanh khoản'),
    ('fpu_seed_operation', 'OPERATION', 'Vận hành');

INSERT INTO mdm_base_rates (id, code, name, attributes) VALUES
    ('brate_seed_repo', 'REPO_NHNN', 'Lãi suất REPO NHNN', '{"currency_code":"VND","source_code":"NHNN"}'),
    ('brate_seed_ref', 'REF_NHNN', 'Lãi suất tái cấp vốn NHNN', '{"currency_code":"VND","source_code":"NHNN"}'),
    ('brate_seed_vndibor', 'VNDIBOR', 'Lãi suất liên ngân hàng VNDIBOR', '{"currency_code":"VND","source_code":"MARKET"}');

INSERT INTO mdm_interest_factors (id, code, name, attributes) VALUES
    ('ifact_seed_360_360', '360/360', 'Quy ước 360 ngày / kỳ 30 ngày', '{"numerator":360,"denominator":360}'),
    ('ifact_seed_365_360', '365/360', 'Quy ước 365 ngày tính trên cơ sở 360', '{"numerator":365,"denominator":360}'),
    ('ifact_seed_365_365', '365/365', 'Quy ước 365 ngày / 365 ngày', '{"numerator":365,"denominator":365}'),
    ('ifact_seed_actual_actual', 'ACT/ACT', 'Ngày thực tế / ngày thực tế', '{"numerator":366,"denominator":366}');

INSERT INTO mdm_cash_denominations (id, code, name, attributes) VALUES
    ('denom_seed_vnd_500k', 'VND-500000', 'Tờ 500.000 đồng', '{"currency_code":"VND","value":500000,"material":"polymer"}'),
    ('denom_seed_vnd_200k', 'VND-200000', 'Tờ 200.000 đồng', '{"currency_code":"VND","value":200000,"material":"polymer"}'),
    ('denom_seed_vnd_100k', 'VND-100000', 'Tờ 100.000 đồng', '{"currency_code":"VND","value":100000,"material":"polymer"}'),
    ('denom_seed_vnd_50k', 'VND-50000', 'Tờ 50.000 đồng', '{"currency_code":"VND","value":50000,"material":"polymer"}'),
    ('denom_seed_vnd_20k', 'VND-20000', 'Tờ 20.000 đồng', '{"currency_code":"VND","value":20000,"material":"polymer"}'),
    ('denom_seed_vnd_10k', 'VND-10000', 'Tờ 10.000 đồng', '{"currency_code":"VND","value":10000,"material":"polymer"}'),
    ('denom_seed_vnd_5k', 'VND-5000', 'Tờ 5.000 đồng', '{"currency_code":"VND","value":5000,"material":"polymer"}'),
    ('denom_seed_vnd_2k', 'VND-2000', 'Tờ 2.000 đồng', '{"currency_code":"VND","value":2000,"material":"polymer"}'),
    ('denom_seed_vnd_1k', 'VND-1000', 'Tờ 1.000 đồng', '{"currency_code":"VND","value":1000,"material":"polymer"}'),
    ('denom_seed_usd_100', 'USD-100', 'Bill 100 USD', '{"currency_code":"USD","value":100,"material":"paper"}'),
    ('denom_seed_usd_50', 'USD-50', 'Bill 50 USD', '{"currency_code":"USD","value":50,"material":"paper"}');

INSERT INTO mdm_scoring_types (id, code, name, description) VALUES
    ('score_seed_cif_corp', 'CIF_CORP', 'Chấm điểm khách hàng doanh nghiệp', 'Bộ điểm mẫu cho khách hàng tổ chức');

INSERT INTO mdm_scoring_indicator_groups (id, code, name, attributes) VALUES
    ('sigr_seed_financial', 'CIF_CORP_FIN', 'Chỉ tiêu tài chính', '{"scoring_type_code":"CIF_CORP","weight":0.5}'),
    ('sigr_seed_biz', 'CIF_CORP_BIZ', 'Chỉ tiêu hoạt động kinh doanh', '{"scoring_type_code":"CIF_CORP","weight":0.3}'),
    ('sigr_seed_credit_hist', 'CIF_CORP_HIST', 'Lịch sử tín dụng', '{"scoring_type_code":"CIF_CORP","weight":0.2}');

INSERT INTO mdm_scoring_indicators (id, code, name, attributes) VALUES
    ('sind_seed_debt_ratio', 'DEBT_RATIO', 'Hệ số nợ / vốn chủ', '{"scoring_type_code":"CIF_CORP","group_code":"CIF_CORP_FIN","weight":0.5,"data_type":"numeric"}'),
    ('sind_seed_liquidity', 'LIQUIDITY_RATIO', 'Hệ số thanh khoản', '{"scoring_type_code":"CIF_CORP","group_code":"CIF_CORP_FIN","weight":0.5,"data_type":"numeric"}'),
    ('sind_seed_years_op', 'YEARS_OPERATING', 'Số năm hoạt động', '{"scoring_type_code":"CIF_CORP","group_code":"CIF_CORP_BIZ","weight":1,"data_type":"numeric"}'),
    ('sind_seed_past_overdue', 'PAST_OVERDUE', 'Từng quá hạn trong 12 tháng', '{"scoring_type_code":"CIF_CORP","group_code":"CIF_CORP_HIST","weight":1,"data_type":"boolean"}');

INSERT INTO mdm_scoring_benchmarks (id, code, name, attributes) VALUES
    ('sbn_seed_corp_a', 'CIF_CORP_A', 'Xếp hạng A - tốt', '{"scoring_type_code":"CIF_CORP","score_min":80,"score_max":100}'),
    ('sbn_seed_corp_b', 'CIF_CORP_B', 'Xếp hạng B - khá', '{"scoring_type_code":"CIF_CORP","score_min":65,"score_max":79}'),
    ('sbn_seed_corp_c', 'CIF_CORP_C', 'Xếp hạng C - trung bình', '{"scoring_type_code":"CIF_CORP","score_min":50,"score_max":64}'),
    ('sbn_seed_corp_d', 'CIF_CORP_D', 'Xếp hạng D - yếu', '{"scoring_type_code":"CIF_CORP","score_min":0,"score_max":49}');

-- +goose Down

DELETE FROM mdm_scoring_benchmarks WHERE id LIKE 'sbn_seed_%';
DELETE FROM mdm_scoring_indicators WHERE id LIKE 'sind_seed_%';
DELETE FROM mdm_scoring_indicator_groups WHERE id LIKE 'sigr_seed_%';
DELETE FROM mdm_scoring_types WHERE id LIKE 'score_seed_%';
DELETE FROM mdm_cash_denominations WHERE id LIKE 'denom_seed_%';
DELETE FROM mdm_interest_factors WHERE id LIKE 'ifact_seed_%';
DELETE FROM mdm_base_rates WHERE id LIKE 'brate_seed_%';
DELETE FROM mdm_fund_purposes WHERE id LIKE 'fpu_seed_%';
DELETE FROM mdm_fund_sources WHERE id LIKE 'fsc_seed_%';
DELETE FROM mdm_loan_contract_types WHERE id LIKE 'lct_seed_%';
DELETE FROM mdm_loan_methods WHERE id LIKE 'lmt_seed_%';
DELETE FROM mdm_industries WHERE id LIKE 'ind_seed_%';
DELETE FROM mdm_economic_types WHERE id LIKE 'ect_seed_%';

DROP TABLE mdm_scoring_benchmarks;
DROP TABLE mdm_scoring_indicators;
DROP TABLE mdm_scoring_indicator_groups;
DROP TABLE mdm_scoring_types;
DROP TABLE mdm_cash_denominations;
DROP TABLE mdm_interest_factors;
DROP TABLE mdm_base_rates;
DROP TABLE mdm_fund_purposes;
DROP TABLE mdm_fund_sources;
DROP TABLE mdm_loan_contract_types;
DROP TABLE mdm_loan_methods;
DROP TABLE mdm_industries;
DROP TABLE mdm_economic_types;
