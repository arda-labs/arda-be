-- +goose Up

-- Rank-score runtime menu (fe_statistical #19) under the statistical group.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_sta_scoring', 'statistical.scoring', 'Xếp hạng điểm', '/statistical/scoring', 'calculator', 'statistical', 'statistical.read', 70)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'statistical' AND tenant_id IS NULL)
WHERE code = 'statistical.scoring' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'statistical.scoring' AND tenant_id IS NULL;
