-- +goose Up

-- Bộ 4 báo cáo RPT (fe_common #27): thẩm định khoản vay, phân loại nợ,
-- khách hàng, kiểm soát vận hành — builders mới trong internal/reports.
-- NOTE: loan/customer builders query lnm_agreements / customers by name; the
-- statistical database must expose those read-only views (ETL step) before
-- they return rows (same prerequisite as LOAN_PORTFOLIO/DEPOSIT_PORTFOLIO).
-- operation_control_summary runs on the statistical DB itself.

INSERT INTO rpt_report_definitions (tenant_id, code, name, group_code, query_id, param_schema, output_format, is_active, created_by)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'LOAN_APPRAISAL', 'Báo cáo thẩm định khoản vay', 'LNM', 'loan_appraisal_summary',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'LOAN_CLASSIFICATION', 'Báo cáo phân loại nợ', 'LNM', 'loan_debt_classification',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'CUSTOMER_SUMMARY', 'Báo cáo khách hàng', 'CRM', 'customer_summary',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'OPERATION_CONTROL', 'Báo cáo kiểm soát vận hành', 'OPS', 'operation_control_summary',
     '{"type":"object","properties":{"period_code":{"type":"string"},"org_code":{"type":"string"}},"required":["period_code"]}',
     'XLSX', true, 'seed')
ON CONFLICT (tenant_id, code) DO UPDATE SET
    query_id = EXCLUDED.query_id, name = EXCLUDED.name, group_code = EXCLUDED.group_code,
    param_schema = EXCLUDED.param_schema, output_format = EXCLUDED.output_format,
    is_active = true, updated_at = now();

-- +goose Down

DELETE FROM rpt_report_definitions
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND code IN ('LOAN_APPRAISAL', 'LOAN_CLASSIFICATION', 'CUSTOMER_SUMMARY', 'OPERATION_CONTROL');
