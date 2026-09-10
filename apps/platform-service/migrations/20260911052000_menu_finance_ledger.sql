-- +goose Up

-- Finance ledger menu (sổ cái / sổ chi tiết) under the finance group.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_fin_ledger', 'finance.ledger', 'Sổ cái / chi tiết', '/finance/ledger', 'list', 'finance', 'finance.read', 60)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL)
WHERE code = 'finance.ledger' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'finance.ledger' AND tenant_id IS NULL;
