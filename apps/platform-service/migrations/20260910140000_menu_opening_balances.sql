-- +goose Up

-- Opening balances screen (Q11/Q2): finance admin page for entering the
-- migration cut-over balances. Route is covered by the finance-read /
-- finance-write policy wildcards; only the menu row is missing.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, parent_id, sort_order)
VALUES
    ('menu_seed_fin_opening', 'finance.opening_balances', 'Số dư đầu kỳ', '/finance/opening-balances', 'file-text', 'finance', 'finance.read', (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL), 65)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

-- +goose Down
DELETE FROM plt_menus WHERE code = 'finance.opening_balances' AND tenant_id IS NULL;
