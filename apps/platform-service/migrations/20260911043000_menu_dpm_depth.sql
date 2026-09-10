-- +goose Up

-- DPM depth menus: rate catalog + batch interest under the deposit group.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_dep_rates',         'deposit.rates',         'Lãi suất huy động',   '/deposit/rates',          'chart', 'deposit', 'deposit.read', 50),
    ('menu_seed_dep_batch_interest','deposit.batch_interest','Trả lãi hàng loạt',   '/deposit/batch-interest', 'list',  'deposit', 'deposit.read', 60)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'deposit' AND tenant_id IS NULL)
WHERE code IN ('deposit.rates', 'deposit.batch_interest') AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code IN ('deposit.rates', 'deposit.batch_interest') AND tenant_id IS NULL;
