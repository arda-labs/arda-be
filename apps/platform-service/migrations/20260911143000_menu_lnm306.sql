-- +goose Up

-- LNM.306 specific provision menu (W7).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_loan_specific_provision', 'loan.specific_provision', 'Dự phòng cụ thể', '/loans/specific-provision', 'shield', 'loan', 'loan.read', 97)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL)
WHERE code = 'loan.specific_provision' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'loan.specific_provision' AND tenant_id IS NULL;
