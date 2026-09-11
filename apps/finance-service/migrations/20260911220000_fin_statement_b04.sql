-- +goose Up

-- B04 (Thuyết minh báo cáo tài chính) — note sections over the pilot COA.
-- Narrative-only sections carry {"type":"none"}; figure sections reuse the
-- accounts formula.

INSERT INTO fin_statement_formula
    (tenant_id, statement_code, row_code, parent_code, label, level, sort_order, sign, formula, is_total, created_by)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'B04', 'NOTES', NULL, 'THUYẾT MINH BÁO CÁO TÀI CHÍNH', 0, 10, 1, '{"type":"none"}', false, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'B04', 'CASH_DETAIL', 'NOTES', 'Tiền và tương đương tiền', 1, 20, 1, '{"type":"accounts","codes":["1011","1131"]}', false, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'B04', 'LOANS_DETAIL', 'NOTES', 'Cho vay khách hàng', 1, 30, 1, '{"type":"accounts","codes":["1311"]}', false, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'B04', 'ACCRUED_DETAIL', 'NOTES', 'Phải thu lãi cho vay', 1, 40, 1, '{"type":"accounts","codes":["13101"]}', false, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'B04', 'PROVISION_DETAIL', 'NOTES', 'Dự phòng phải thu khó đòi', 1, 50, -1, '{"type":"accounts","codes":["1319"]}', false, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'B04', 'INCOME_DETAIL', 'NOTES', 'Doanh thu lãi và phí', 1, 60, -1, '{"type":"accounts","codes":["5111","5112"]}', false, 'seed')
ON CONFLICT (tenant_id, statement_code, row_code) DO NOTHING;

-- +goose Down

DELETE FROM fin_statement_formula
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'B04';
