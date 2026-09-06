-- +goose Up

-- Complete the seeded global tree (EPAS com_cfg_menu parity): the first
-- migration only planted children under the admin group, so the DB-driven
-- sidebar showed every other group as a flat leaf. Children mirror the shell
-- static fallback (apps/shell/src/config/nav-config.ts) and reuse icon names
-- from the shared menu icon registry.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    -- Finance
    ('menu_seed_fin_accounts',    'finance.accounts',           'Tài khoản',            '/finance/accounts',            'coins',    'finance',  'finance.read', 10),
    ('menu_seed_fin_transactions','finance.transactions',       'Giao dịch',            '/finance/transactions',        'file-text','finance',  'finance.read', 20),
    ('menu_seed_fin_approvals',   'finance.approvals',          'Phê duyệt',            '/finance/approvals',           'shield',   'finance',  'finance.read', 30),
    ('menu_seed_fin_trial',       'finance.trial_balance',      'Bảng cân đối thử',     '/finance/trial-balance',       'dashboard','finance',  'finance.read', 40),
    ('menu_seed_fin_coa',         'finance.accounting_config',  'Cấu hình kế toán',     '/finance/accounting-config',   'settings', 'finance',  'finance.read', 50),
    -- HRM
    ('menu_seed_hrm_positions',   'hrm.positions',              'Chức vụ',              '/hrm/positions',               'users',    'hrm',      'hrm.read', 10),
    ('menu_seed_hrm_job_titles',  'hrm.job_titles',             'Chức danh',            '/hrm/job-titles',              'file-text','hrm',      'hrm.read', 20),
    ('menu_seed_hrm_org_units',   'hrm.org_units',              'Cơ cấu tổ chức',       '/hrm/org-units',               'list',     'hrm',      'hrm.read', 30),
    ('menu_seed_hrm_registrations','hrm.registrations',         'Đăng ký nhân sự',      '/hrm/registrations',           'file-text','hrm',      'hrm.read', 40),
    ('menu_seed_hrm_employees',   'hrm.employees',              'Thông tin nhân sự',    '/hrm/employees',               'users',    'hrm',      'hrm.read', 50),
    -- Customers
    ('menu_seed_cus_registrations','customers.registrations',   'Đăng ký khách hàng',   '/customers/registrations',     'file-text','crm',      'crm.read', 10),
    ('menu_seed_cus_profiles',    'customers.profiles',         'Hồ sơ khách hàng',     '/customers/profiles',          'contact',  'crm',      'crm.read', 20),
    ('menu_seed_cus_risk_cases',  'customers.risk_cases',       'Khách hàng rủi ro',    '/customers/risk-cases',        'shield',   'crm',      'crm.read', 30),
    -- Workbench
    ('menu_seed_wb_drafts',       'workbench.drafts',           'Nháp chưa hoàn thành', '/workbench/drafts',            'file-text','workflow', '', 10),
    ('menu_seed_wb_incoming',     'workbench.incoming',         'Giao dịch đến',        '/workbench/incoming-transactions','inbox', 'workflow', '', 20),
    ('menu_seed_wb_outgoing',     'workbench.outgoing',         'Giao dịch đi',         '/workbench/outgoing-transactions','inbox', 'workflow', '', 30),
    ('menu_seed_wb_search',       'workbench.search',           'Tìm kiếm giao dịch',   '/workbench/transaction-search','list',     'workflow', '', 40),
    -- Workflow
    ('menu_seed_wf_case_types',   'workflow.case_types',        'Loại hồ sơ',           '/workflow/case-types',         'list',     'workflow', 'workflow.read', 10),
    ('menu_seed_wf_configs',      'workflow.process_configs',   'Cấu hình quy trình',   '/workflow/process-configs',    'settings', 'workflow', 'workflow.read', 20),
    ('menu_seed_wf_sla',          'workflow.sla_policies',      'Chính sách SLA',       '/workflow/sla-policies',       'clock',    'workflow', 'workflow.read', 30),
    ('menu_seed_wf_templates',    'workflow.description_templates','Mẫu mô tả',         '/workflow/description-templates','file-text','workflow','workflow.read', 40),
    ('menu_seed_wf_roles',        'workflow.roles',             'Vai trò quy trình',    '/workflow/roles',              'users',    'workflow', 'workflow.read', 50),
    ('menu_seed_wf_monitoring',   'workflow.monitoring',        'Giám sát quy trình',   '/workflow/monitoring',         'dashboard','workflow', 'workflow.read', 60),
    -- AI Center (group without its own route; permission strings are
    -- comma-separated and expanded client-side)
    ('menu_seed_ai_center',       'ai_center',                  'AI Center',            '',                             'sparkles', 'ai',       'ai.admin,ai.knowledge.manage,superadmin,platform.manage', 75),
    ('menu_seed_ai_knowledge',    'ai_center.knowledge',        'Nguồn tri thức',       '/ai/knowledge',                'book',     'ai',       'ai.admin,ai.knowledge.manage,superadmin,platform.manage', 10),
    ('menu_seed_ai_settings',     'ai_center.settings',         'Cài đặt AI',           '/ai/settings',                 'sliders',  'ai',       'ai.admin,superadmin,platform.manage', 20),
    ('menu_seed_ai_approvals',    'ai_center.approvals',        'Phê duyệt & Kiểm toán','/ai/approvals',                'shield',   'ai',       'ai.admin,superadmin,platform.manage', 30),
    ('menu_seed_ai_tools',        'ai_center.tools',            'Công cụ & MCP Registry','/ai/tools',                   'wrench',   'ai',       'ai.admin,superadmin,platform.manage', 40),
    ('menu_seed_ai_analytics',    'ai_center.analytics',        'Giám sát & Chi phí',   '/ai/analytics',                'dashboard','ai',       'ai.admin,superadmin,platform.manage', 50),
    ('menu_seed_ai_agents',       'ai_center.agents',           'Agent Studio',         '/ai/agents',                   'bot',      'ai',       'ai.admin,superadmin,platform.manage', 60),
    -- Menu configuration page (CRUD over this very table)
    ('menu_seed_admin_menus',     'admin.menus',                'Cấu hình menu',        '/admin/menus',                 'list',     'platform', 'platform.manage', 95),
    -- Geo / reference pages previously only in the static fallback
    ('menu_seed_admin_provinces', 'admin.provinces',            'Tỉnh thành',           '/admin/provinces',             'building', 'platform', 'platform.read', 91),
    ('menu_seed_admin_wards',     'admin.wards',                'Phường xã',            '/admin/wards',                 'building', 'platform', 'platform.read', 92),
    ('menu_seed_admin_area_types','admin.area_types',           'Loại khu vực',         '/admin/area-types',            'list',     'platform', 'platform.manage', 93),
    ('menu_seed_admin_areas',     'admin.areas',                'Khu vực',              '/admin/areas',                 'building', 'platform', 'platform.read', 94),
    ('menu_seed_admin_credit_ins','admin.credit_institutions',  'Tổ chức tín dụng',     '/admin/credit-institutions',   'building', 'platform', 'platform.read', 96),
    ('menu_seed_admin_templates', 'admin.templates',            'Mẫu biểu hệ thống',    '/admin/templates',             'file-text','platform', 'platform.manage', 97),
    ('menu_seed_admin_calendar',  'admin.calendar',             'Lịch hệ thống & EOD',  '/admin/calendar',              'clock',    'platform', 'platform.manage', 98),
    ('menu_seed_admin_cutoff',    'admin.cutoff',               'Cấu hình Cut-off Time','/admin/cutoff',                'clock',    'platform', 'platform.manage', 99)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

-- Link children to their parents (by code; id column values are internal).
UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL)
WHERE code LIKE 'finance.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'hrm' AND tenant_id IS NULL)
WHERE code LIKE 'hrm.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'customers' AND tenant_id IS NULL)
WHERE code LIKE 'customers.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'workbench' AND tenant_id IS NULL)
WHERE code LIKE 'workbench.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'workflow' AND tenant_id IS NULL)
WHERE code LIKE 'workflow.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'ai_center' AND tenant_id IS NULL)
WHERE code LIKE 'ai_center.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin' AND tenant_id IS NULL)
WHERE code LIKE 'admin.%' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE tenant_id IS NULL AND code IN (
    'finance.accounts', 'finance.transactions', 'finance.approvals',
    'finance.trial_balance', 'finance.accounting_config',
    'hrm.positions', 'hrm.job_titles', 'hrm.org_units', 'hrm.registrations', 'hrm.employees',
    'customers.registrations', 'customers.profiles', 'customers.risk_cases',
    'workbench.drafts', 'workbench.incoming', 'workbench.outgoing', 'workbench.search',
    'workflow.case_types', 'workflow.process_configs', 'workflow.sla_policies',
    'workflow.description_templates', 'workflow.roles', 'workflow.monitoring',
    'ai_center', 'ai_center.knowledge', 'ai_center.settings', 'ai_center.approvals',
    'ai_center.tools', 'ai_center.analytics', 'ai_center.agents',
    'admin.menus', 'admin.provinces', 'admin.wards', 'admin.area_types',
    'admin.areas', 'admin.credit_institutions', 'admin.templates',
    'admin.calendar', 'admin.cutoff'
);
