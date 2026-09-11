-- +goose Up

-- CMMS compliance run menu (fe_statistical #21) under the statistical group.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_sta_cmms', 'statistical.cmms', 'CMMS', '/statistical/cmms', 'shield-check', 'statistical', 'statistical.read', 90)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'statistical' AND tenant_id IS NULL)
WHERE code = 'statistical.cmms' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'statistical.cmms' AND tenant_id IS NULL;
