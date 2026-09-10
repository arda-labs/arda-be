-- +goose Up

-- CFM lifecycle menus: fund-type + product catalogs under the capital group.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_cap_fund_types', 'capital.fund_types', 'Loại vốn',     '/capital/fund-types', 'list',  'capital', 'capital.read', 20),
    ('menu_seed_cap_products',   'capital.products',   'Sản phẩm vốn', '/capital/products',   'coins', 'capital', 'capital.read', 30)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'capital' AND tenant_id IS NULL)
WHERE code IN ('capital.fund_types', 'capital.products') AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code IN ('capital.fund_types', 'capital.products') AND tenant_id IS NULL;
