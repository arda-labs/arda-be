-- +goose Up

-- Interbank BORROWING indicators (step 6b-2, PCF topic "Tiền vay TCTD") that
-- resolve from the fact the ETL now materialises.
--
-- The catalog splits this topic by lender class (NHHTX / NHNN / other TCTD /
-- safety fund) and by funding purpose. Both are first-class columns on the fact
-- and exposed as dimensions, so the split is a filter away — but the codes are
-- seeded from Arda's own taxonomy (lender_type / funding_purpose), NOT from the
-- PCF's Oracle organisation codes, so the totals and the controlled buckets are
-- seeded and the free-text lender names are left to the counterparty dimension.
--
-- Amounts read the fact columns 1:1 (minor units, no scale).

INSERT INTO rpt_indicators
    (tenant_id, code, name, unit, group_code, created_by,
     kpi_type, periodicity, is_ratio, root_code, root_name, meaning,
     sources, formula, dimensions, display_format, rounding_digits)
VALUES
    ('00000000-0000-0000-0000-000000000010', '70000.01', 'Tổng tiền vay TCTD', 'VND', 'Tiền vay TCTD', 'seed',
     'P', 'D', false, '70000', 'Tổng tiền vay TCTD',
     'Tổng số dư tiền vay tại các tổ chức tín dụng',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_borrow_daily","column":"outstanding_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","lender_type","funding_purpose","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '70000.02', 'Tổng tiền vay TCTD trong hạn', 'VND', 'Tiền vay TCTD', 'seed',
     'P', 'D', false, '70000', 'Tổng tiền vay TCTD',
     'Số dư tiền vay chưa đến hạn thanh toán',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_borrow_daily","column":"outstanding_minor","filter":{"status":"ACTIVE","maturity_status":"CURRENT"},"as_of":"period_end","scale":1}',
     '["org","lender_type","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '70000.03', 'Tổng tiền vay TCTD quá hạn', 'VND', 'Tiền vay TCTD', 'seed',
     'P', 'D', false, '70000', 'Tổng tiền vay TCTD',
     'Số dư tiền vay đã đến hạn nhưng chưa thanh toán',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_borrow_daily","column":"outstanding_minor","filter":{"status":"ACTIVE","maturity_status":"OVERDUE"},"as_of":"period_end","scale":1}',
     '["org","lender_type","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '70001.01', 'Tiền vay NHHTX', 'VND', 'Tiền vay TCTD', 'seed',
     'P', 'D', false, '70001', 'Tiền vay NHHTX',
     'Tổng số dư tiền vay tại Ngân hàng Hợp tác xã',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_borrow_daily","column":"outstanding_minor","filter":{"status":"ACTIVE","lender_type":"NHHTX"},"as_of":"period_end","scale":1}',
     '["org","funding_purpose","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '70007.01', 'Vay quỹ bảo toàn hệ thống', 'VND', 'Tiền vay TCTD', 'seed',
     'P', 'D', false, '70007', 'Vay quỹ bảo toàn hệ thống',
     'Tổng số dư vay từ Quỹ bảo toàn hệ thống',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_borrow_daily","column":"outstanding_minor","filter":{"status":"ACTIVE","lender_type":"SAFETY_FUND"},"as_of":"period_end","scale":1}',
     '["org","funding_purpose","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '70005.01', 'Số món vay TCTD', 'món', 'Tiền vay TCTD', 'seed',
     'P', 'D', false, '70005', 'Số món vay TCTD',
     'Số hợp đồng vay đang hoạt động tại tổ chức tín dụng',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"count","fact":"rpt_fact_ibm_borrow_daily","filter":{"status":"ACTIVE"},"as_of":"period_end"}',
     '["org","lender_type","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '70004.01', 'Lãi suất bình quân tiền vay TCTD', '%/năm', 'Tiền vay TCTD', 'seed',
     'P', 'D', true, '70004', 'Lãi suất tiền vay TCTD',
     'Lãi suất bình quân các khoản vay tại tổ chức tín dụng',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"average","fact":"rpt_fact_ibm_borrow_daily","column":"interest_rate","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","lender_type","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '70002.01', 'Tiền vay TCTD theo kỳ hạn ban đầu', 'VND', 'Tiền vay TCTD', 'seed',
     'P', 'D', false, '70002', 'Tiền vay TCTD theo kỳ hạn ban đầu',
     'Số dư tiền vay tại tổ chức tín dụng phân theo kỳ hạn ban đầu của hợp đồng',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_borrow_daily","column":"outstanding_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","lender_type","term","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '70003.01', 'Lãi dự chi tiền vay TCTD', 'VND', 'Tiền vay TCTD', 'seed',
     'P', 'D', false, '70003', 'Lãi dự chi tiền vay TCTD',
     'Lãi dự chi của các khoản vay tại tổ chức tín dụng',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"sum","fact":"rpt_fact_ibm_borrow_daily","column":"accrued_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","lender_type","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '70000.01.02', 'Tăng trưởng tiền vay TCTD', '%', 'Tiền vay TCTD', 'seed',
     'C', 'M', true, '70000', 'Tổng tiền vay TCTD',
     'Tăng trưởng tổng tiền vay TCTD so với kỳ trước',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"growth","indicator":"70000.01","compare":"previous_period","as_of":"period_end","percent":true}',
     '["org","time"]', 'percent', 2),
    ('00000000-0000-0000-0000-000000000010', '70000.01.03', 'Trung bình tiền vay TCTD 3 tháng gần nhất', 'VND', 'Tiền vay TCTD', 'seed',
     'C', 'M', false, '70000', 'Tổng tiền vay TCTD',
     'Số dư tiền vay TCTD bình quân 3 tháng gần nhất',
     '["rpt_fact_ibm_borrow_daily"]',
     '{"type":"trailing_average","indicator":"70000.01","periods":3,"as_of":"period_end","scale":1}',
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
  AND code IN ('70000.01','70000.02','70000.03','70001.01','70007.01','70005.01',
               '70004.01','70002.01','70003.01','70000.01.02','70000.01.03');
