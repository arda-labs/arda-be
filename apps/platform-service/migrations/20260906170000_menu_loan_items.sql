-- +goose Up

-- Loan admin menu entries (mdm entries seeded in create_menus).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_loan', 'admin.loan', 'Tín dụng', '/admin/loan', 'coins', 'platform', 'loan.read', 110)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin' AND tenant_id IS NULL)
WHERE code = 'admin.loan' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down
DELETE FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL;
