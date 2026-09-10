-- +goose Up

-- Report definition seeds (Q8): the two builders currently implemented in
-- statistical-service (loan portfolio by debt group / deposit portfolio by
-- product). Run/export endpoints validate parameters against param_schema.
-- NOTE: builders query lnm_agreements / dpm_savings by name; the statistical
-- database must expose those read-only views (ETL step) before the reports
-- return rows — deployment prerequisite recorded in docs/screen-gap-matrix.md.

INSERT INTO rpt_report_definitions (tenant_id, code, name, group_code, query_id, param_schema, output_format, is_active, created_by)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'LOAN_PORTFOLIO', 'Dư nợ theo nhóm nợ', 'LNM', 'loan_portfolio_summary',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'DEPOSIT_PORTFOLIO', 'Huy động theo sản phẩm', 'DPM', 'deposit_portfolio',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed')
ON CONFLICT (tenant_id, code) DO UPDATE SET
    query_id = EXCLUDED.query_id, param_schema = EXCLUDED.param_schema,
    output_format = EXCLUDED.output_format, is_active = true, updated_at = now();

-- +goose Down

DELETE FROM rpt_report_definitions
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND code IN ('LOAN_PORTFOLIO', 'DEPOSIT_PORTFOLIO');
