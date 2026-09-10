-- +goose Up

-- General provision screen (LNM.307.01): per-branch provision period with
-- preview + approval case. Route is covered by the loan-read/write policy
-- wildcards; only the menu row is missing.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, parent_id, sort_order)
VALUES
    ('menu_seed_ln_general_provision', 'loan.general_provision', 'Dự phòng chung', '/loans/general-provision', 'file-text', 'loan', 'loan.read', (SELECT id FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL), 80)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

-- +goose Down
DELETE FROM plt_menus WHERE code = 'loan.general_provision' AND tenant_id IS NULL;
