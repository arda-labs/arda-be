-- +goose Up

-- Loan report menus (sổ khoản vay / sao kê / tài sản bảo đảm).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_loan_reports', 'loan.reports', 'Báo cáo tín dụng', '/loans/reports', 'chart', 'loan', 'loan.read', 90)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL)
WHERE code = 'loan.reports' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'loan.reports' AND tenant_id IS NULL;
