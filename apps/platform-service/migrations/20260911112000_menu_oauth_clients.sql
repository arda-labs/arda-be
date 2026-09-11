-- +goose Up

-- OAuth2 client registry menu (X3).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_iam_oauth_clients', 'admin.oauth_clients', 'Client tích hợp', '/admin/oauth-clients', 'shield', 'iam', 'iam.user.read', 97)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin' AND tenant_id IS NULL)
WHERE code = 'admin.oauth_clients' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'admin.oauth_clients' AND tenant_id IS NULL;
