-- +goose Up

-- Scheduled jobs / EOD operations menu (W6).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_plt_jobs', 'platform.jobs', 'Job định kỳ', '/admin/jobs', 'settings', 'platform', 'platform.read', 90)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin' AND tenant_id IS NULL)
WHERE code = 'platform.jobs' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'platform.jobs' AND tenant_id IS NULL;
