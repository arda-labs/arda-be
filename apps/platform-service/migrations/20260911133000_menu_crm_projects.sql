-- +goose Up

-- CRM projects menu (W7).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_cus_projects', 'customers.projects', 'Dự án', '/customers/projects', 'folder', 'crm', 'crm.read', 35)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'customers' AND tenant_id IS NULL)
WHERE code = 'customers.projects' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'customers.projects' AND tenant_id IS NULL;
