-- +goose Up

-- Deposit reports menu (sổ tiền gửi / giao dịch / liên ngân hàng).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_dep_reports', 'deposit.reports', 'Báo cáo tiền gửi', '/deposit/reports', 'chart', 'deposit', 'deposit.read', 70)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'deposit' AND tenant_id IS NULL)
WHERE code = 'deposit.reports' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'deposit.reports' AND tenant_id IS NULL;
