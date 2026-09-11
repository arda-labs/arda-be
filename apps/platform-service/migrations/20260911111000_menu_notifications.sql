-- +goose Up

-- Notification templates + sender config menu (X2).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_plt_notifications', 'platform.notifications', 'Thông báo & mail', '/admin/notifications', 'settings', 'platform', 'platform.read', 97)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin' AND tenant_id IS NULL)
WHERE code = 'platform.notifications' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'platform.notifications' AND tenant_id IS NULL;
