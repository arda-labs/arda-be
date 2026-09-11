-- +goose Up

-- Working hours menu (W6a).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_plt_working_hours', 'platform.working_hours', 'Giờ làm việc', '/admin/working-hours', 'settings', 'platform', 'platform.read', 95)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin' AND tenant_id IS NULL)
WHERE code = 'platform.working_hours' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'platform.working_hours' AND tenant_id IS NULL;
