-- +goose Up

-- Finance cash book menu (VCM sổ quỹ tiền mặt, W7).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_fin_cash', 'finance.cash', 'Sổ quỹ tiền mặt', '/finance/cash', 'coins', 'finance', 'finance.read', 75)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL)
WHERE code = 'finance.cash' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'finance.cash' AND tenant_id IS NULL;
