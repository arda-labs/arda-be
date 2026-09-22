-- +goose Up

-- Derived loan/deposit indicators (PCF topics "Tín dụng" and "Huy động vốn")
-- that resolve from the facts already materialised by the reporting ETL.
--
-- Scope: only the indicators whose base is an existing Arda indicator or a
-- plain aggregate over rpt_fact_loan_agreement_daily /
-- rpt_fact_deposit_contract_daily. The catalog's many "theo kỳ hạn",
-- "theo phương thức", "theo địa bàn", "nông nghiệp/phi nông nghiệp" splits are
-- NOT seeded: those need term/method/region/sector columns the facts do not
-- carry yet, and inventing them would put wrong numbers on a report.
--
-- Growth and trailing_average reference the BASE INDICATOR CODE: the engine
-- loads that code's stored values across periods (ComputeSeries), so the base
-- must be computed for the period before these resolve — see the series note in
-- docs/reporting-data-layer.md §9c.
--
-- Codes stay clean rather than copying the catalog's Excel float noise
-- (e.g. 20000.009999999998) or the reserved 30001.* range already used by
-- "Số khoản vay đang hoạt động".

INSERT INTO rpt_indicators
    (tenant_id, code, name, unit, group_code, created_by,
     kpi_type, periodicity, is_ratio, root_code, root_name, meaning,
     sources, formula, dimensions, display_format, rounding_digits)
VALUES
    -- ── Huy động vốn ──
    ('00000000-0000-0000-0000-000000000010', '20000.01.01', 'Trung bình số dư tiền gửi 3 tháng gần nhất', 'VND', 'Huy động vốn', 'seed',
     'C', 'M', false, '20000', 'Tổng số dư tiền gửi',
     'Số dư tiền gửi bình quân của 3 tháng gần nhất',
     '["rpt_fact_deposit_contract_daily"]',
     '{"type":"trailing_average","indicator":"20000.01","periods":3,"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '20000.01.02', 'Tăng trưởng số dư tiền gửi', '%', 'Huy động vốn', 'seed',
     'C', 'M', true, '20000', 'Tổng số dư tiền gửi',
     'Tăng trưởng số dư tiền gửi so với kỳ trước',
     '["rpt_fact_deposit_contract_daily"]',
     '{"type":"growth","indicator":"20000.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '20007.01', 'Số người gửi tiền', 'người', 'Huy động vốn', 'seed',
     'P', 'D', false, '20007', 'Số người gửi tiền',
     'Số khách hàng đang có số dư tiền gửi lớn hơn 0',
     '["rpt_fact_deposit_contract_daily"]',
     '{"type":"count_distinct","fact":"rpt_fact_deposit_contract_daily","column":"customer_code","filter":{"status":"ACTIVE","principal_minor":{"op":">","value":"0"}},"as_of":"period_end"}',
     '["org","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '20007.01.02', 'Tăng trưởng số người gửi tiền', '%', 'Huy động vốn', 'seed',
     'C', 'M', true, '20007', 'Số người gửi tiền',
     'Tăng trưởng số người gửi tiền so với kỳ trước',
     '["rpt_fact_deposit_contract_daily"]',
     '{"type":"growth","indicator":"20007.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '20019.01', 'Số dư tiền gửi bình quân/người gửi tiền', 'VND', 'Huy động vốn', 'seed',
     'P', 'D', false, '20019', 'Số dư tiền gửi bình quân/người gửi tiền',
     'Số dư tiền gửi bình quân trên một người gửi tiền',
     '["rpt_fact_deposit_contract_daily"]',
     '{"type":"ratio","as_of":"period_end","numerator":{"type":"sum","fact":"rpt_fact_deposit_contract_daily","column":"principal_minor","filter":{"status":"ACTIVE"}},"denominator":{"type":"count_distinct","fact":"rpt_fact_deposit_contract_daily","column":"customer_code","filter":{"status":"ACTIVE"}}}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '20019.01.02', 'Tăng trưởng số dư tiền gửi bình quân/người', '%', 'Huy động vốn', 'seed',
     'C', 'M', true, '20019', 'Số dư tiền gửi bình quân/người gửi tiền',
     'Tăng trưởng số dư bình quân trên một người gửi tiền so với kỳ trước',
     '["rpt_fact_deposit_contract_daily"]',
     '{"type":"growth","indicator":"20019.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),

    -- ── Tín dụng ──
    ('00000000-0000-0000-0000-000000000010', '30000.01.02', 'Tăng trưởng dư nợ cho vay', '%', 'Tín dụng', 'seed',
     'C', 'M', true, '30000', 'Tổng dư nợ cho vay',
     'Tăng trưởng tổng dư nợ cho vay so với kỳ trước',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"growth","indicator":"30000.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '30000.01.03', 'Trung bình dư nợ cho vay 3 tháng gần nhất', 'VND', 'Tín dụng', 'seed',
     'C', 'M', false, '30000', 'Tổng dư nợ cho vay',
     'Dư nợ cho vay bình quân của 3 tháng gần nhất',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"trailing_average","indicator":"30000.01","periods":3,"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30020.01.01', 'Trung bình dư nợ xấu 3 tháng gần nhất', 'VND', 'Tín dụng', 'seed',
     'C', 'M', false, '30020', 'Dư nợ xấu (nhóm 3-5)',
     'Dư nợ xấu bình quân của 3 tháng gần nhất',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"trailing_average","indicator":"30020.01","periods":3,"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30020.01.02', 'Tăng trưởng dư nợ xấu', '%', 'Tín dụng', 'seed',
     'C', 'M', true, '30020', 'Dư nợ xấu (nhóm 3-5)',
     'Tăng trưởng dư nợ xấu so với kỳ trước',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"growth","indicator":"30020.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '30021.01.02', 'Tăng trưởng dự phòng rủi ro tín dụng', '%', 'Tín dụng', 'seed',
     'C', 'M', true, '30021', 'Dự phòng rủi ro tín dụng',
     'Tăng trưởng dự phòng rủi ro tín dụng so với kỳ trước',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"growth","indicator":"30021.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2)
ON CONFLICT (tenant_id, code) DO UPDATE SET
    name = EXCLUDED.name, unit = EXCLUDED.unit, group_code = EXCLUDED.group_code,
    kpi_type = EXCLUDED.kpi_type, periodicity = EXCLUDED.periodicity,
    is_ratio = EXCLUDED.is_ratio, root_code = EXCLUDED.root_code, root_name = EXCLUDED.root_name,
    meaning = EXCLUDED.meaning, sources = EXCLUDED.sources, formula = EXCLUDED.formula,
    dimensions = EXCLUDED.dimensions, display_format = EXCLUDED.display_format,
    rounding_digits = EXCLUDED.rounding_digits, updated_at = now();

-- The interbank growth indicator was seeded without `percent`, so it stored the
-- raw ratio (0.5) against a '%' unit. Align it with the growth convention the
-- rest of the seeds use.
UPDATE rpt_indicators
SET formula = '{"type":"growth","indicator":"60000.01","compare":"previous_period","as_of":"period_end","percent":true}',
    updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND code = '60000.01.02';

-- +goose Down
DELETE FROM rpt_indicators
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND code IN ('20000.01.01','20000.01.02','20007.01','20007.01.02','20019.01','20019.01.02',
               '30000.01.02','30000.01.03','30020.01.01','30020.01.02','30021.01.02');
