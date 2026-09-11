-- +goose Up

-- Capital fund-source report menu.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_cap_reports', 'capital.reports', 'Báo cáo nguồn vốn', '/capital/reports', 'chart', 'capital', 'capital.read', 40)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'capital' AND tenant_id IS NULL)
WHERE code = 'capital.reports' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'capital.reports' AND tenant_id IS NULL;
