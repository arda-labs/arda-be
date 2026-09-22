-- +goose Up

-- Loan indicators sliced by the term bucket the fact now carries. Codes come
-- from the PCF catalog ("Dư nợ cho vay ngắn hạn" / "trung dài hạn").
--
-- Scope: only the term split. The catalog's industry ("nông nghiệp / phi nông
-- nghiệp") and method ("từng lần / hạn mức / cầm cố") indicators are NOT seeded
-- yet: the fact now carries industry_code and loan_method_code, but the allowed
-- value taxonomy is not established in Arda, and guessing which code means
-- "nông nghiệp" would mis-bucket loans on a regulatory report. The dimensions
-- ("industry", "loan_method") are declared so those indicators are a filter away
-- once the taxonomy is fixed.
--
-- Term buckets are produced by the ETL from the contract term:
--   DEMAND (term 0) | SHORT (<= 12 months) | MEDIUM_LONG (> 12 months).

INSERT INTO rpt_indicators
    (tenant_id, code, name, unit, group_code, created_by,
     kpi_type, periodicity, is_ratio, root_code, root_name, meaning,
     sources, formula, dimensions, display_format, rounding_digits)
VALUES
    ('00000000-0000-0000-0000-000000000010', '30002.01', 'Dư nợ cho vay ngắn hạn', 'VND', 'Tín dụng', 'seed',
     'P', 'D', false, '30002', 'Dư nợ cho vay theo kỳ hạn',
     'Tổng dư nợ cho vay có kỳ hạn ban đầu từ 12 tháng trở xuống',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor","filter":{"status":"ACTIVE","term_bucket":"SHORT"},"as_of":"period_end","scale":1}',
     '["org","loan_method","industry","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30002.02', 'Dư nợ cho vay trung dài hạn', 'VND', 'Tín dụng', 'seed',
     'P', 'D', false, '30002', 'Dư nợ cho vay theo kỳ hạn',
     'Tổng dư nợ cho vay có kỳ hạn ban đầu trên 12 tháng',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor","filter":{"status":"ACTIVE","term_bucket":"MEDIUM_LONG"},"as_of":"period_end","scale":1}',
     '["org","loan_method","industry","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30002.03', 'Dư nợ cho vay không kỳ hạn', 'VND', 'Tín dụng', 'seed',
     'P', 'D', false, '30002', 'Dư nợ cho vay theo kỳ hạn',
     'Tổng dư nợ cho vay không kỳ hạn (kỳ hạn 0)',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor","filter":{"status":"ACTIVE","term_bucket":"DEMAND"},"as_of":"period_end","scale":1}',
     '["org","loan_method","industry","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30002.01.02', 'Tăng trưởng dư nợ cho vay ngắn hạn', '%', 'Tín dụng', 'seed',
     'C', 'M', true, '30002', 'Dư nợ cho vay theo kỳ hạn',
     'Tăng trưởng dư nợ cho vay ngắn hạn so với kỳ trước',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"growth","indicator":"30002.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '30002.02.02', 'Tăng trưởng dư nợ cho vay trung dài hạn', '%', 'Tín dụng', 'seed',
     'C', 'M', true, '30002', 'Dư nợ cho vay theo kỳ hạn',
     'Tăng trưởng dư nợ cho vay trung dài hạn so với kỳ trước',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"growth","indicator":"30002.02","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '30002.01.03', 'Trung bình dư nợ cho vay ngắn hạn 3 tháng', 'VND', 'Tín dụng', 'seed',
     'C', 'M', false, '30002', 'Dư nợ cho vay theo kỳ hạn',
     'Dư nợ cho vay ngắn hạn bình quân 3 tháng gần nhất',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"trailing_average","indicator":"30002.01","periods":3,"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30002.02.03', 'Trung bình dư nợ cho vay trung dài hạn 3 tháng', 'VND', 'Tín dụng', 'seed',
     'C', 'M', false, '30002', 'Dư nợ cho vay theo kỳ hạn',
     'Dư nợ cho vay trung dài hạn bình quân 3 tháng gần nhất',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"trailing_average","indicator":"30002.02","periods":3,"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2)
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
  AND code IN ('30002.01','30002.02','30002.03','30002.01.02','30002.02.02','30002.01.03','30002.02.03');
