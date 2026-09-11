-- +goose Up

-- Quỹ (fund) menu: trích lập / sử dụng quỹ.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_fin_funds', 'finance.funds', 'Quỹ', '/finance/funds', 'wallet', 'finance', 'finance.read', 70)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL)
WHERE code = 'finance.funds' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'finance.funds' AND tenant_id IS NULL;
