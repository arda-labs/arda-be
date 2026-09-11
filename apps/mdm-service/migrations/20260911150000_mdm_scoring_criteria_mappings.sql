-- +goose Up

-- W5d: scoring criteria mapping catalog (EPAS CtgCfgScoringIndcMapp). One row
-- links an indicator to a scoring type/criteria with a weight; the generic
-- mdm registry exposes CRUD automatically.

CREATE TABLE IF NOT EXISTS mdm_scoring_criteria_mappings (
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
CREATE UNIQUE INDEX IF NOT EXISTS mdm_scoring_criteria_mappings_code_uk
    ON mdm_scoring_criteria_mappings (code, COALESCE(tenant_id, ''));

-- Seed the corporate scoring set mappings (reference data; tenant rows free).
INSERT INTO mdm_scoring_criteria_mappings (id, tenant_id, code, name, attributes) VALUES
    ('scm_seed_corp_fin_debt',   NULL, 'CIF_CORP_FIN_DEBT',   'Chỉ tiêu tài chính - Nhóm nợ',        '{"scoring_type_code":"CIF_CORP","indicator_code":"FIN_DEBT_GROUP","criteria_code":"FINANCIAL","weight":0.3}'),
    ('scm_seed_corp_fin_profit', NULL, 'CIF_CORP_FIN_PROFIT', 'Chỉ tiêu tài chính - Lợi nhuận',      '{"scoring_type_code":"CIF_CORP","indicator_code":"FIN_PROFIT","criteria_code":"FINANCIAL","weight":0.3}'),
    ('scm_seed_corp_coll_value', NULL, 'CIF_CORP_COLL_VALUE', 'Chỉ tiêu TSBĐ - Giá trị',             '{"scoring_type_code":"CIF_CORP","indicator_code":"COLL_VALUE","criteria_code":"COLLATERAL","weight":0.4}')
ON CONFLICT (id) DO NOTHING;

-- +goose Down

DROP TABLE IF EXISTS mdm_scoring_criteria_mappings;
