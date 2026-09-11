-- +goose Up

-- B03-DN (Lưu chuyển tiền tệ) — first statement using the movement/opening
-- formula types added to the statement engine (period movement from
-- fin_trial_balance_daily.incr_*, opening balance before from_date). The
-- direct-method section rows (operating/investing/financing detail) require
-- the CF classification taxonomy and are a follow-up; this seed delivers the
-- closed cash position: opening + net movement = closing over cash/settlement
-- accounts (1011 tiền mặt, 1131 tiền gửi).

INSERT INTO fin_statement_formula
    (tenant_id, statement_code, row_code, parent_code, label, level, sort_order, sign, formula, is_total, created_by)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'B03', 'CASHFLOW', NULL, 'LƯU CHUYỂN TIỀN TỆ', 0, 10, 1, '{"type":"none"}', false, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'B03', 'OPENING_CASH', 'CASHFLOW', 'Tiền và tương đương tiền đầu kỳ', 1, 20, 1, '{"type":"opening","codes":["1011","1131"]}', false, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'B03', 'NET_CASH', 'CASHFLOW', 'Lưu chuyển tiền thuần trong kỳ', 1, 30, 1, '{"type":"movement","codes":["1011","1131"]}', false, 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'B03', 'CLOSING_CASH', 'CASHFLOW', 'Tiền và tương đương tiền cuối kỳ', 1, 40, 1, '{"type":"rows","members":[{"code":"OPENING_CASH"},{"code":"NET_CASH"}]}', true, 'seed')
ON CONFLICT (tenant_id, statement_code, row_code) DO NOTHING;

-- +goose Down

DELETE FROM fin_statement_formula
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'B03';
