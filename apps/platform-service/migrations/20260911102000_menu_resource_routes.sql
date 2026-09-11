-- +goose Up

-- Resource-management route-policy browser menu (W6a).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_iam_resource_routes', 'admin.resource_routes', 'Tài nguyên & policy', '/admin/resource-routes', 'shield', 'iam', 'iam.user.read', 95)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin' AND tenant_id IS NULL)
WHERE code = 'admin.resource_routes' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'admin.resource_routes' AND tenant_id IS NULL;
