-- +goose Up

-- Loan plan management menu (W7).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_loan_plans', 'loan.plans', 'Kế hoạch vay', '/loans/plans', 'list', 'loan', 'loan.read', 95)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL)
WHERE code = 'loan.plans' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'loan.plans' AND tenant_id IS NULL;
