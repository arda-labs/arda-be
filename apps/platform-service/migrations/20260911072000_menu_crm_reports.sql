-- +goose Up

-- CRM customer report menu.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_cus_reports', 'customers.reports', 'Báo cáo khách hàng', '/customers/reports', 'chart', 'crm', 'crm.read', 40)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'customers' AND tenant_id IS NULL)
WHERE code = 'customers.reports' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'customers.reports' AND tenant_id IS NULL;
