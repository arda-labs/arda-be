-- +goose Up

-- Counterparty master menu (TK đối tác) under the finance group.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_fin_counterparties', 'finance.counterparties', 'Tài khoản đối tác', '/finance/counterparties', 'contact', 'finance', 'finance.read', 70)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL)
WHERE code = 'finance.counterparties' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'finance.counterparties' AND tenant_id IS NULL;
