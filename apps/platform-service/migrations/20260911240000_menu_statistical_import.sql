-- +goose Up

-- QCMS import transaction staging menu (fe_statistical #20).

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_sta_import', 'statistical.import', 'Nhập liệu báo cáo', '/statistical/import', 'upload', 'statistical', 'statistical.read', 80)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'statistical' AND tenant_id IS NULL)
WHERE code = 'statistical.import' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE code = 'statistical.import' AND tenant_id IS NULL;
