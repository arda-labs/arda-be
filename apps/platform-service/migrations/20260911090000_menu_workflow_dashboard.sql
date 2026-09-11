-- +goose Up

-- Workflow dashboard menu (W6).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_wf_dashboard', 'workflow.dashboard', 'Bảng điều khiển', '/workflow/dashboard', 'dashboard', 'workflow', 'workflow.read', 5)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'workflow' AND tenant_id IS NULL)
WHERE code = 'workflow.dashboard' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'workflow.dashboard' AND tenant_id IS NULL;
