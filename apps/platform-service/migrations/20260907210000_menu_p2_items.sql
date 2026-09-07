-- +goose Up

-- P2 menu items: deposit/capital/statistical remotes, loan lifecycle children
-- (P1b), mdm interest rates, finance journal; retire finance legacy pages
-- (transactions/approvals) dropped in Phase 0. Icon names come from
-- packages/ui/src/config/menu-icons.ts.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    -- Deposit (DPM + IBM)
    ('menu_seed_dep_savings',   'deposit.savings',   'Tiết kiệm',           '/deposit',             'coins',  'deposit',  'deposit.read',  10),
    ('menu_seed_dep_products',  'deposit.products',  'Sản phẩm tiền gửi',   '/deposit/products',    'list',   'deposit',  'deposit.read',  20),
    ('menu_seed_dep_interbank', 'deposit.interbank', 'Liên ngân hàng',      '/deposit/interbank',   'building','deposit', 'deposit.read',  30),
    -- Capital (CFM)
    ('menu_seed_cap_contracts', 'capital.contracts', 'Hợp đồng vốn',        '/capital',             'coins',  'capital',  'capital.read',  10),
    -- Statistical (QCMS + report framework)
    ('menu_seed_sta_definitions','statistical.report_definitions','Định nghĩa báo cáo','/statistical','chart', 'statistical','statistical.read', 10),
    ('menu_seed_sta_indicators','statistical.indicators','Chỉ tiêu thống kê',  '/statistical/indicators','dashboard','statistical','statistical.read', 20),
    ('menu_seed_sta_submissions','statistical.submissions','Nộp báo cáo',     '/statistical/submissions','file-text','statistical','statistical.read', 30),
    -- Loan lifecycle children (P1b) under the admin.loan group path
    ('menu_seed_ln_products',   'loan.products',     'Sản phẩm tín dụng',   '/loans/products',      'list',   'loan',     'loan.read',     10),
    ('menu_seed_ln_disbursements','loan.disbursements','Giải ngân',          '/loans/disbursements', 'coins',  'loan',     'loan.read',     20),
    ('menu_seed_ln_collections','loan.collections',  'Thu nợ',              '/loans/collections',   'inbox',  'loan',     'loan.read',     30),
    ('menu_seed_ln_vfu',        'loan.vfu',          'VFU',                 '/loans/vfu',           'file-text','loan',   'loan.read',     40),
    -- MDM interest rates
    ('menu_seed_mdm_rates',     'mdm.interest_rates','Lãi suất',            '/admin/mdm/interest-rates','clock','mdm',    'mdm.read',      20),
    -- Finance journal (P1a) + deactivation of Phase-0-removed pages
    ('menu_seed_fin_journal',   'finance.journal',   'Sổ nhật ký',          '/finance/journal',     'file-text','finance','finance.read',  60)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

-- Retire legacy finance pages removed in Phase 0 (rebuild: no fallback path).
UPDATE plt_menus SET is_active = false
WHERE code IN ('finance.transactions', 'finance.approvals') AND tenant_id IS NULL;

UPDATE plt_menus SET path = '/finance', icon = 'coins'
WHERE code = 'finance.accounts' AND tenant_id IS NULL AND path <> '/finance/accounts';

-- Fix the loan admin leaf: point at the lifecycle index, keep permission.
UPDATE plt_menus SET title = 'Tín dụng', path = '/loans', icon = 'coins'
WHERE code = 'admin.loan' AND tenant_id IS NULL;

-- Top-level groups for the three new remotes (must exist before the
-- children-linking UPDATEs below).
INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, sort_order) VALUES
    ('menu_seed_deposit',     'deposit',     'Tiền gửi',        '/deposit',     'coins', 'deposit',     'deposit.read',     110),
    ('menu_seed_capital',     'capital',     'Vốn',             '/capital',     'coins', 'capital',     'capital.read',     120),
    ('menu_seed_statistical', 'statistical', 'Thống kê & Báo cáo','/statistical','chart','statistical', 'statistical.read', 130)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

-- Link children to their parents (by code).
UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'deposit' AND tenant_id IS NULL)
WHERE code LIKE 'deposit.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'capital' AND tenant_id IS NULL)
WHERE code LIKE 'capital.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'statistical' AND tenant_id IS NULL)
WHERE code LIKE 'statistical.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL)
WHERE code LIKE 'loan.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'admin.mdm' AND tenant_id IS NULL)
WHERE code LIKE 'mdm.%' AND tenant_id IS NULL AND parent_id IS NULL;

UPDATE plt_menus SET parent_id = (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL)
WHERE code LIKE 'finance.%' AND tenant_id IS NULL AND parent_id IS NULL;

-- +goose Down

DELETE FROM plt_menus WHERE tenant_id IS NULL AND code IN (
    'deposit.savings', 'deposit.products', 'deposit.interbank',
    'capital.contracts',
    'statistical.report_definitions', 'statistical.indicators', 'statistical.submissions',
    'loan.products', 'loan.disbursements', 'loan.collections', 'loan.vfu',
    'mdm.interest_rates', 'finance.journal',
    'deposit', 'capital', 'statistical'
);

UPDATE plt_menus SET is_active = true
WHERE code IN ('finance.transactions', 'finance.approvals') AND tenant_id IS NULL;
