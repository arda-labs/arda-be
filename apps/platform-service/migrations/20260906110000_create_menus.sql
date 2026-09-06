-- +goose Up

-- DB-driven navigation (EPAS com_cfg_menu parity, decision D2 in
-- docs/epas-to-arda-matrix.md): the MFE shell renders its sidebar from
-- plt_menus instead of hardcoded route tables. tenant_id IS NULL rows are the
-- global default tree; tenants may add their own entries or deactivate
-- inherited ones by redefining the same code with tenant scope.

CREATE TABLE plt_menus (
    id                  VARCHAR(64) PRIMARY KEY,
    tenant_id           VARCHAR(64),
    parent_id           VARCHAR(64),
    code                VARCHAR(100) NOT NULL,
    title               VARCHAR(255) NOT NULL,
    path                VARCHAR(255) NOT NULL DEFAULT '',
    icon                VARCHAR(100) NOT NULL DEFAULT '',
    remote              VARCHAR(50) NOT NULL DEFAULT '',
    required_permission VARCHAR(100) NOT NULL DEFAULT '',
    sort_order          INTEGER NOT NULL DEFAULT 0,
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX plt_menus_code_uk ON plt_menus (code, COALESCE(tenant_id, ''));
CREATE INDEX plt_menus_parent_idx ON plt_menus (parent_id, sort_order);

-- Global default tree mirroring the shell static routes (seed = migration-
-- managed configuration; runtime writes are tenant-scoped only).
INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_dashboard',  'dashboard',        'Dashboard',            '/',            'dashboard',   '',         '', 0),
    ('menu_seed_admin',      'admin',            'Quản trị hệ thống',    '/admin',       'settings',    '',         '', 10),
    ('menu_seed_users',      'admin.users',      'Người dùng',           '/admin/users', 'users',       'iam',      'iam.read', 10),
    ('menu_seed_groups',     'admin.groups',     'Nhóm',                 '/admin/groups','groups',      'iam',      'iam.read', 20),
    ('menu_seed_roles',      'admin.roles',      'Vai trò',              '/admin/roles', 'shield',      'iam',      'iam.read', 30),
    ('menu_seed_permissions','admin.permissions','Quyền hạn',            '/admin/permissions','key',    'iam',      'iam.read', 40),
    ('menu_seed_audit',      'admin.audit',      'Kiểm toán',            '/admin/audit', 'log',         'iam',      'iam.read', 50),
    ('menu_seed_tenants',    'admin.tenants',    'Tenant',               '/admin/tenants','building',   'iam',      'iam.read', 60),
    ('menu_seed_org',        'admin.organizations','Tổ chức',            '/admin/organizations','org',  'platform', 'platform.read', 70),
    ('menu_seed_parameters', 'admin.parameters', 'Tham số hệ thống',     '/admin/parameters','sliders', 'platform', 'platform.read', 80),
    ('menu_seed_lookups',    'admin.lookups',    'Danh mục lookup',      '/admin/lookups','list',       'platform', 'platform.read', 90),
    ('menu_seed_mdm',        'admin.mdm',        'Danh mục nghiệp vụ',   '/admin/mdm',   'book',        'mdm',      'mdm.read', 100),
    ('menu_seed_finance',    'finance',          'Tài chính',            '/finance',     'coins',       'finance',  'finance.read', 20),
    ('menu_seed_hrm',        'hrm',              'Nhân sự',              '/hrm',         'id-card',     'hrm',      'hrm.read', 30),
    ('menu_seed_customers',  'customers',        'Khách hàng',           '/customers',   'contact',     'crm',      'crm.read', 40),
    ('menu_seed_workflow',   'workflow',         'Quy trình duyệt',      '/workflow',    'workflow',    'workflow', 'workflow.read', 50),
    ('menu_seed_workbench',  'workbench',        'Bộ xử lý giao dịch',   '/workbench',   'inbox',       'workflow', 'workflow.read', 60),
    ('menu_seed_ai',         'ai',               'Trợ lý AI',            '/ai',          'sparkles',    'ai',       'ai.assistant.use', 70)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin' AND tenant_id IS NULL)
WHERE code LIKE 'admin.%' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE tenant_id IS NULL;
DROP TABLE IF EXISTS plt_menus;
