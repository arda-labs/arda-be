-- +goose Up

-- IBM product catalog menu under the deposit group.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_dep_ibm_products', 'deposit.ibm_products', 'Sản phẩm liên ngân hàng', '/deposit/interbank/products', 'list', 'deposit', 'deposit.read', 40)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'deposit' AND tenant_id IS NULL)
WHERE code = 'deposit.ibm_products' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'deposit.ibm_products' AND tenant_id IS NULL;
