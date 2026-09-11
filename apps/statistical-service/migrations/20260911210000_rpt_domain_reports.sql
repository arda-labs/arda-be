-- +goose Up

-- Bộ 5 báo cáo nghiệp vụ DPM/CFM/TSBĐ (fe_loan #26) over the same Q8 builder
-- pattern; builders query dpm_savings / cfc_contracts / cfc_movements /
-- lnm_collaterals by name (statistical DB read-only views ETL prerequisite).

INSERT INTO rpt_report_definitions (tenant_id, code, name, group_code, query_id, param_schema, output_format, is_active, created_by)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'DPM_MATURITY_LADDER', 'Huy động theo tháng đáo hạn', 'DPM', 'deposit_maturity_ladder',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_ACCRUED', 'Lãi dự chi theo sản phẩm', 'DPM', 'deposit_accrued_by_product',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'CAPITAL_BY_FUND_TYPE', 'Nguồn vốn theo loại quỹ', 'CFM', 'capital_by_fund_type',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'CAPITAL_MOVEMENTS', 'Biến động nguồn vốn trong kỳ', 'CFM', 'capital_movements',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'COLLATERAL_BY_TYPE', 'Tài sản bảo đảm theo loại', 'LNM', 'collateral_by_type',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed')
ON CONFLICT (tenant_id, code) DO UPDATE SET
    query_id = EXCLUDED.query_id, name = EXCLUDED.name, group_code = EXCLUDED.group_code,
    param_schema = EXCLUDED.param_schema, output_format = EXCLUDED.output_format,
    is_active = true, updated_at = now();

-- +goose Down

DELETE FROM rpt_report_definitions
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND code IN ('DPM_MATURITY_LADDER', 'DPM_ACCRUED', 'CAPITAL_BY_FUND_TYPE', 'CAPITAL_MOVEMENTS', 'COLLATERAL_BY_TYPE');
