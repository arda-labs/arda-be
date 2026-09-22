-- +goose Up

-- Customer-count indicators (PCF topic "Khách hàng").
--
-- The catalog counts customers by relationship, not just "all customers":
-- members vs non-members, customers with a deposit, customers with a loan. The
-- ETL stamps those flags on rpt_fact_customer_daily, so each is a plain count
-- with a filter plus its growth pair.
--
-- Not seeded here: the "trong địa bàn / ngoài địa bàn" splits (need a customer
-- region, which the customer projection does not carry) and the "chuyển tiền"
-- counts (need the transfer domain).
--
-- Note: the catalog lists 10007.01 twice (once for "đang gửi tiền có vốn góp
-- chờ duyệt", once for "đang vay vốn"). This seeds the vay vốn meaning; the
-- vốn góp chờ duyệt variant needs a member-request join and would collide on
-- the code, so it is deliberately left out rather than silently renumbered.

INSERT INTO rpt_indicators
    (tenant_id, code, name, unit, group_code, created_by,
     kpi_type, periodicity, is_ratio, root_code, root_name, meaning,
     sources, formula, dimensions, display_format, rounding_digits)
VALUES
    ('00000000-0000-0000-0000-000000000010', '10001.01', 'Số lượng khách hàng', 'khách hàng', 'Khách hàng', 'seed',
     'P', 'D', false, '10001', 'Số lượng khách hàng',
     'Tổng số khách hàng đang hoạt động',
     '["rpt_fact_customer_daily"]',
     '{"type":"count","fact":"rpt_fact_customer_daily","filter":{"status":"ACTIVE"},"as_of":"period_end"}',
     '["org","segment","customer_type","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10001.02.01', 'Tăng trưởng số lượng khách hàng', '%', 'Khách hàng', 'seed',
     'C', 'M', true, '10001', 'Số lượng khách hàng',
     'Tăng trưởng số lượng khách hàng so với kỳ trước',
     '["rpt_fact_customer_daily"]',
     '{"type":"growth","indicator":"10001.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '10002.01', 'Số lượng khách hàng là thành viên', 'khách hàng', 'Khách hàng', 'seed',
     'P', 'D', false, '10002', 'Số lượng khách hàng là thành viên',
     'Số khách hàng đồng thời là thành viên góp vốn',
     '["rpt_fact_customer_daily"]',
     '{"type":"count","fact":"rpt_fact_customer_daily","filter":{"status":"ACTIVE","is_member":"Y"},"as_of":"period_end"}',
     '["org","segment","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10002.02.01', 'Tăng trưởng số lượng khách hàng là thành viên', '%', 'Khách hàng', 'seed',
     'C', 'M', true, '10002', 'Số lượng khách hàng là thành viên',
     'Tăng trưởng số khách hàng là thành viên so với kỳ trước',
     '["rpt_fact_customer_daily"]',
     '{"type":"growth","indicator":"10002.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '10003.01', 'Số lượng khách hàng ngoài thành viên', 'khách hàng', 'Khách hàng', 'seed',
     'P', 'D', false, '10003', 'Số lượng khách hàng ngoài thành viên',
     'Số khách hàng không phải thành viên góp vốn',
     '["rpt_fact_customer_daily"]',
     '{"type":"count","fact":"rpt_fact_customer_daily","filter":{"status":"ACTIVE","is_member":"N"},"as_of":"period_end"}',
     '["org","segment","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10003.02.01', 'Tăng trưởng số lượng khách hàng ngoài thành viên', '%', 'Khách hàng', 'seed',
     'C', 'M', true, '10003', 'Số lượng khách hàng ngoài thành viên',
     'Tăng trưởng số khách hàng ngoài thành viên so với kỳ trước',
     '["rpt_fact_customer_daily"]',
     '{"type":"growth","indicator":"10003.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '10006.01', 'Số lượng khách hàng đang gửi tiền', 'khách hàng', 'Khách hàng', 'seed',
     'P', 'D', false, '10006', 'Số lượng khách hàng đang gửi tiền',
     'Số khách hàng đang có số dư tiền gửi lớn hơn 0',
     '["rpt_fact_customer_daily"]',
     '{"type":"count","fact":"rpt_fact_customer_daily","filter":{"status":"ACTIVE","has_deposit":"Y"},"as_of":"period_end"}',
     '["org","segment","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10006.02.01', 'Tăng trưởng số lượng khách hàng đang gửi tiền', '%', 'Khách hàng', 'seed',
     'C', 'M', true, '10006', 'Số lượng khách hàng đang gửi tiền',
     'Tăng trưởng số khách hàng đang gửi tiền so với kỳ trước',
     '["rpt_fact_customer_daily"]',
     '{"type":"growth","indicator":"10006.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '10007.01', 'Số lượng khách hàng đang vay vốn', 'khách hàng', 'Khách hàng', 'seed',
     'P', 'D', false, '10007', 'Số lượng khách hàng đang vay vốn',
     'Số khách hàng đang có dư nợ vay lớn hơn 0',
     '["rpt_fact_customer_daily"]',
     '{"type":"count","fact":"rpt_fact_customer_daily","filter":{"status":"ACTIVE","has_loan":"Y"},"as_of":"period_end"}',
     '["org","segment","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10007.02.01', 'Tăng trưởng số lượng khách hàng đang vay vốn', '%', 'Khách hàng', 'seed',
     'C', 'M', true, '10007', 'Số lượng khách hàng đang vay vốn',
     'Tăng trưởng số khách hàng đang vay vốn so với kỳ trước',
     '["rpt_fact_customer_daily"]',
     '{"type":"growth","indicator":"10007.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2)
ON CONFLICT (tenant_id, code) DO UPDATE SET
    name = EXCLUDED.name, unit = EXCLUDED.unit, group_code = EXCLUDED.group_code,
    kpi_type = EXCLUDED.kpi_type, periodicity = EXCLUDED.periodicity,
    is_ratio = EXCLUDED.is_ratio, root_code = EXCLUDED.root_code, root_name = EXCLUDED.root_name,
    meaning = EXCLUDED.meaning, sources = EXCLUDED.sources, formula = EXCLUDED.formula,
    dimensions = EXCLUDED.dimensions, display_format = EXCLUDED.display_format,
    rounding_digits = EXCLUDED.rounding_digits, updated_at = now();

-- Remove the mis-seeded customer indicator: code 60001.01 sits in the wrong
-- group (it belongs to the accounting topic) and its growth referenced
-- 60000.01, the interbank DEPOSIT indicator, so it reported deposit growth
-- under a customer name.
DELETE FROM rpt_indicators
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND code = '60001.01' AND group_code = 'Khách hàng';

-- +goose Down
DELETE FROM rpt_indicators
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND code IN ('10001.01','10001.02.01','10002.01','10002.02.01','10003.01','10003.02.01',
               '10006.01','10006.02.01','10007.01','10007.02.01');
