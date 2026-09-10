-- +goose Up

-- Composite loan dossier screen (EPAS loan-management): read-only view of a
-- contract's agreements, repay schedule, movements, collateral and cases.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, parent_id, sort_order)
VALUES
    ('menu_seed_ln_dossier', 'loan.dossier', 'Hồ sơ khoản vay', '/loans/dossier', 'file-text', 'loan', 'loan.read', (SELECT id FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL), 70)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

-- +goose Down
DELETE FROM plt_menus WHERE code = 'loan.dossier' AND tenant_id IS NULL;
