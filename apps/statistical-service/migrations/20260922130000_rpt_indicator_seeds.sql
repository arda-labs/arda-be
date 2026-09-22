-- +goose Up

-- Seed the QCMS indicator catalog for the pilot tenant from the PCF catalog
-- (docs/epas-survey/exports/pcf-kpi-catalog.json — 923 indicators, PCF-10-KPIs.xlsx).
--
-- Scope note: this seeds only the indicators whose data sources Arda already
-- models (loan / deposit / capital / crm + the GL), so every seeded kpi_type='P'
-- has a resolvable fact table. Indicators that need domains Arda has not built
-- yet (member/equity, interbank borrow/deposit, cash/transfer) are intentionally
-- NOT seeded — they would resolve to nothing and violate the "no fake data"
-- invariant. They stay in the JSON as the backlog for step 6.
--
-- `formula` is declarative JSON: {type:"growth"|"average"|"ratio"|"sum", ...}
-- with members referencing indicators/fact columns — never SQL text (Q8).

INSERT INTO rpt_indicators
    (tenant_id, code, name, unit, group_code, created_by,
     kpi_type, periodicity, is_ratio, root_code, root_name, meaning,
     sources, formula, dimensions, display_format, rounding_digits)
VALUES
    ('00000000-0000-0000-0000-000000000010', '20000.01', 'Tổng số dư tiền gửi', 'VND', 'Huy động vốn', 'seed',
     'P', 'D', false, '20000', 'Số dư tiền gửi khách hàng',
     'Tổng số dư tiền gửi của khách hàng tại một thời điểm',
     '["rpt_fact_deposit_contract_daily"]',
     '{"type":"sum","fact":"rpt_fact_deposit_contract_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '20020.02', 'Số dư tiền gửi bình quân/người gửi tiền', 'VND', 'Huy động vốn', 'seed',
     'C', 'D', false, '20020', 'Số dư tiền gửi khách hàng / người gửi tiền',
     'Một người gửi tiền có số dư bình quân tại Quỹ là bao nhiêu',
     '["rpt_fact_deposit_contract_daily"]',
     '{"type":"ratio","numerator":{"type":"sum","fact":"rpt_fact_deposit_contract_daily","column":"principal_minor","filter":{"status":"ACTIVE"}},"denominator":{"type":"count_distinct","fact":"rpt_fact_deposit_contract_daily","column":"customer_code","filter":{"status":"ACTIVE"}},"as_of":"period_end"}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30000.01', 'Tổng dư nợ cho vay', 'VND', 'Tín dụng', 'seed',
     'P', 'D', false, '30000', 'Tổng dư nợ cho vay',
     'Tổng dư nợ cho vay khách hàng tại một thời điểm',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","time","product"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30001.01', 'Số khoản vay đang hoạt động', 'khoản', 'Tín dụng', 'seed',
     'P', 'D', false, '30001', 'Số khoản vay',
     'Số khế ước vay đang còn dư nợ',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"count","fact":"rpt_fact_loan_agreement_daily","filter":{"status":"ACTIVE"},"as_of":"period_end"}',
     '["org","time","product"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '30020.01', 'Dư nợ xấu (nhóm 3-5)', 'VND', 'Tín dụng', 'seed',
     'P', 'D', false, '30020', 'Dư nợ xấu',
     'Tổng dư nợ thuộc nhóm nợ 3, 4, 5',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor","filter":{"status":"ACTIVE","debt_group_code":["GROUP_3","GROUP_4","GROUP_5"]},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30020.02', 'Tỷ lệ nợ xấu / tổng dư nợ', '%', 'Tín dụng', 'seed',
     'C', 'D', true, '30020', 'Tỷ lệ nợ xấu',
     'Tỷ lệ nợ xấu trên tổng dư nợ cho vay',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"ratio","numerator":{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor","filter":{"debt_group_code":["GROUP_3","GROUP_4","GROUP_5"]}},"denominator":{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor"},"as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '30021.01', 'Dự phòng rủi ro tín dụng', 'VND', 'Tín dụng', 'seed',
     'P', 'D', false, '30021', 'Dự phòng rủi ro',
     'Tổng số dự phòng đã trích lập cho dư nợ cho vay',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"provision_amt_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '30021.02', 'Tỷ lệ bao phủ dự phòng nợ xấu', '%', 'Tín dụng', 'seed',
     'C', 'D', true, '30021', 'Tỷ lệ bao phủ dự phòng',
     'Dự phòng đã trích lập trên dư nợ xấu',
     '["rpt_fact_loan_agreement_daily"]',
     '{"type":"ratio","numerator":{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"provision_amt_minor","filter":{"status":"ACTIVE"}},"denominator":{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor","filter":{"debt_group_code":["GROUP_3","GROUP_4","GROUP_5"]}},"as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '40000.01', 'Tổng nguồn vốn huy động', 'VND', 'Nguồn vốn', 'seed',
     'P', 'D', false, '40000', 'Nguồn vốn',
     'Tổng số dư hợp đồng nguồn vốn đang hoạt động',
     '["rpt_fact_capital_contract_daily"]',
     '{"type":"sum","fact":"rpt_fact_capital_contract_daily","column":"amount_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","time","fund_type"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '40001.01', 'Số hợp đồng nguồn vốn', 'hợp đồng', 'Nguồn vốn', 'seed',
     'P', 'D', false, '40001', 'Số hợp đồng nguồn vốn',
     'Số hợp đồng nguồn vốn đang hoạt động',
     '["rpt_fact_capital_contract_daily"]',
     '{"type":"count","fact":"rpt_fact_capital_contract_daily","filter":{"status":"ACTIVE"},"as_of":"period_end"}',
     '["org","time","fund_type"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '50000.01', 'Tổng giá trị tài sản bảo đảm', 'VND', 'TSĐB', 'seed',
     'P', 'D', false, '50000', 'Tài sản bảo đảm',
     'Tổng giá trị tài sản bảo đảm đang quản lý',
     '["rpt_fact_loan_collateral_daily"]',
     '{"type":"sum","fact":"rpt_fact_loan_collateral_daily","column":"coll_value_minor","as_of":"period_end","scale":1}',
     '["org","time","coll_type"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '60000.01', 'Số lượng khách hàng', 'khách hàng', 'Khách hàng', 'seed',
     'P', 'D', false, '60000', 'Số lượng khách hàng',
     'Tổng số khách hàng đang hoạt động',
     '["rpt_fact_customer_daily"]',
     '{"type":"count","fact":"rpt_fact_customer_daily","filter":{"status":"ACTIVE"},"as_of":"period_end"}',
     '["org","time","segment"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '60001.01', 'Tăng trưởng số lượng khách hàng', '%', 'Khách hàng', 'seed',
     'C', 'M', true, '60001', 'Tăng trưởng khách hàng',
     'Tỷ lệ tăng trưởng số lượng khách hàng so với kỳ trước',
     '["rpt_fact_customer_daily"]',
     '{"type":"growth","indicator":"60000.01","compare":"previous_period","percent":true}',
     '["org","time","segment"]', 'percent', 2)
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
  AND code IN ('20000.01','20020.02','30000.01','30001.01','30020.01','30020.02',
               '30021.01','30021.02','40000.01','40001.01','50000.01','60000.01','60001.01');
