-- +goose Up

-- Seed the interbank-deposit indicators (step 6b, PCF topic "Tiền gửi TCTD").
-- Codes come from docs/epas-survey/exports/pcf-kpi-catalog.json.
--
-- Scope note: the PCF source splits 60000/60001/60002 by counterparty class
-- (NHHT / NHNN / other credit institution). That split needs the credit
-- institution registry (platform plt_credit_institutions), which is not
-- populated yet, so it is NOT hardcoded here. The fact carries counterparty_code
-- and the "counterparty" dimension is declared, so a flat total works today and
-- the per-class rows can be added as one more filter each once the registry is
-- authoritative. Seeding a made-up employer code would put a wrong number on a
-- regulatory report.
--
-- Amounts read the fact columns 1:1 (minor units, no scale) — see the
-- 20260922140000 scale fix. Rates are the fact's own numeric column.

INSERT INTO rpt_indicators
    (tenant_id, code, name, unit, group_code, created_by,
     kpi_type, periodicity, is_ratio, root_code, root_name, meaning,
     sources, formula, dimensions, display_format, rounding_digits)
VALUES
    ('00000000-0000-0000-0000-000000000010', '60000.01', 'Tiền gửi tại TCTD', 'VND', 'Tiền gửi TCTD', 'seed',
     'P', 'D', false, '60000', 'Tiền gửi tại TCTD',
     'Tổng số dư tiền gửi của Quỹ tại các tổ chức tín dụng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","counterparty","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '60002.01', 'Tiền gửi tại các TCTD khác', 'VND', 'Tiền gửi TCTD', 'seed',
     'P', 'D', false, '60002', 'Tiền gửi tại các TCTD khác',
     'Số dư tiền gửi tại tổ chức tín dụng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","counterparty","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '60002.02', 'Tiền gửi tại TCTD khác có kỳ hạn', 'VND', 'Tiền gửi TCTD', 'seed',
     'P', 'D', false, '60002', 'Tiền gửi tại các TCTD khác',
     'Số dư tiền gửi có kỳ hạn tại tổ chức tín dụng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE","term_months":{"op":">","value":"0"}},"as_of":"period_end","scale":1}',
     '["org","counterparty","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '60002.03', 'Tiền gửi tại TCTD khác không kỳ hạn', 'VND', 'Tiền gửi TCTD', 'seed',
     'P', 'D', false, '60002', 'Tiền gửi tại các TCTD khác',
     'Số dư tiền gửi không kỳ hạn tại tổ chức tín dụng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE","term_months":"0"},"as_of":"period_end","scale":1}',
     '["org","counterparty","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '60004.01', 'Số hợp đồng tiền gửi tại TCTD', 'hợp đồng', 'Tiền gửi TCTD', 'seed',
     'P', 'D', false, '60004', 'Lãi suất tiền gửi tại TCTD',
     'Số lượng hợp đồng tiền gửi đang hoạt động tại tổ chức tín dụng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"count","fact":"rpt_fact_ibm_deposit_daily","filter":{"status":"ACTIVE"},"as_of":"period_end"}',
     '["org","counterparty","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '60004.369999999799', 'Lãi suất bình quân tiền gửi tại TCTD', '%/năm', 'Tiền gửi TCTD', 'seed',
     'P', 'D', true, '60004', 'Lãi suất tiền gửi tại TCTD',
     'Lãi suất bình quân gia quyền theo số dư tiền gửi tại tổ chức tín dụng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"average","fact":"rpt_fact_ibm_deposit_daily","column":"interest_rate","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","counterparty","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '60000.01.02', 'Tăng trưởng tiền gửi tại TCTD', '%', 'Tiền gửi TCTD', 'seed',
     'C', 'D', true, '60000', 'Tiền gửi tại TCTD',
     'Tăng trưởng tiền gửi tại tổ chức tín dụng so với kỳ trước',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"growth","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","counterparty","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '60000.01.01', 'Trung bình tiền gửi tại TCTD 3 tháng gần nhất', 'VND', 'Tiền gửi TCTD', 'seed',
     'C', 'D', false, '60000', 'Tiền gửi tại TCTD',
     'Số dư tiền gửi bình quân tại tổ chức tín dụng trong 3 tháng gần nhất',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"trailing_average","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1,"periods":3}',
     '["org","counterparty","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '60003.01', 'Tiền gửi tại TCTD theo kỳ hạn ban đầu', 'VND', 'Tiền gửi TCTD', 'seed',
     'P', 'D', false, '60003', 'Tiền gửi tại TCTD theo kỳ hạn ban đầu',
     'Số dư tiền gửi tại tổ chức tín dụng phân theo kỳ hạn ban đầu của hợp đồng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","counterparty","term","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '60002.04', 'Lãi dự chi tiền gửi tại TCTD', 'VND', 'Tiền gửi TCTD', 'seed',
     'P', 'D', false, '60002', 'Tiền gửi tại các TCTD khác',
     'Lãi dự chi của tiền gửi tại tổ chức tín dụng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_deposit_daily","column":"accrued_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","counterparty","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '60003.02', 'Tiền gửi tại TCTD có kỳ hạn theo lãi suất', 'VND', 'Tiền gửi TCTD', 'seed',
     'P', 'D', false, '60003', 'Tiền gửi tại TCTD theo kỳ hạn ban đầu',
     'Số dư tiền gửi có kỳ hạn tại tổ chức tín dụng phân theo lãi suất hợp đồng',
     '["rpt_fact_ibm_deposit_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE","term_months":{"op":">","value":"0"}},"as_of":"period_end","scale":1}',
     '["org","counterparty","time"]', 'amount', 2)
ON CONFLICT (tenant_id, code) DO UPDATE SET
    name = EXCLUDED.name, unit = EXCLUDED.unit, group_code = EXCLUDED.group_code,
    kpi_type = EXCLUDED.kpi_type, periodicity = EXCLUDED.periodicity,
    is_ratio = EXCLUDED.is_ratio, root_code = EXCLUDED.root_code, root_name = EXCLUDED.root_name,
    meaning = EXCLUDED.meaning, sources = EXCLUDED.sources, formula = EXCLUDED.formula,
    dimensions = EXCLUDED.dimensions, display_format = EXCLUDED.display_format,
    rounding_digits = EXCLUDED.rounding_digits, updated_at = now();

-- +goose Down
DELETE FROM rpt_indicators
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND code IN ('60000.01','60000.01.01','60000.01.02','60002.01','60002.02','60002.03',
               '60002.04','60003.01','60003.02','60004.01','60004.369999999799');
