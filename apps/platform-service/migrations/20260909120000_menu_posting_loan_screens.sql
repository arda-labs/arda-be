-- +goose Up

-- Iteration 13/14 menu additions: the five FAC posting screens (EPAS
-- "danh mục kế toán") grouped under a new finance.posting sub-parent,
-- finance statements, and the loan lifecycle screens added with the batch
-- rework (adjustments hub + repay plan). Icon names come from
-- packages/ui/src/config/menu-icons.ts.

INSERT INTO plt_menus (id, code, title, path, icon, remote, required_permission, parent_id, sort_order)
VALUES
    -- Finance: posting group (5 FAC screens) as a sub-parent, statements leaf
    ('menu_seed_fin_posting',        'finance.posting',            'Bút toán kế toán',    '/finance/posting',                'book',   'finance', 'finance.read', (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL), 15),
    ('menu_seed_fin_posting_single', 'finance.posting_single',     'Bút toán lẻ',         '/finance/posting/single-entry',   'file-text', 'finance', 'finance.read', (SELECT id FROM plt_menus WHERE code = 'finance.posting' AND tenant_id IS NULL), 10),
    ('menu_seed_fin_posting_double', 'finance.posting_double',     'Bút toán kép đỏ đen', '/finance/posting/double-entry',   'file-text', 'finance', 'finance.read', (SELECT id FROM plt_menus WHERE code = 'finance.posting' AND tenant_id IS NULL), 20),
    ('menu_seed_fin_posting_offbal', 'finance.posting_off_balance','Ngoại bảng',          '/finance/posting/off-balance',    'file-text', 'finance', 'finance.read', (SELECT id FROM plt_menus WHERE code = 'finance.posting' AND tenant_id IS NULL), 30),
    ('menu_seed_fin_posting_closing','finance.posting_closing',    'Kết chuyển thu chi',  '/finance/posting/closing',        'file-text', 'finance', 'finance.read', (SELECT id FROM plt_menus WHERE code = 'finance.posting' AND tenant_id IS NULL), 40),
    ('menu_seed_fin_posting_cancel', 'finance.posting_cancellation','Hủy giao dịch',      '/finance/posting/cancellation',   'file-text', 'finance', 'finance.read', (SELECT id FROM plt_menus WHERE code = 'finance.posting' AND tenant_id IS NULL), 50),
    ('menu_seed_fin_statements',     'finance.statements',         'Báo cáo tài chính',   '/finance/statements',             'chart',  'finance', 'finance.read', (SELECT id FROM plt_menus WHERE code = 'finance' AND tenant_id IS NULL), 70),
    -- Loan lifecycle: adjustment kinds hub + repay plan (under admin.loan)
    ('menu_seed_ln_adjustments',     'loan.adjustments',           'Điều chỉnh khoản vay','/loans/adjustments',              'sliders','loan',    'loan.read',    (SELECT id FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL), 50),
    ('menu_seed_ln_repay_plan',      'loan.repay_plan',            'Kế hoạch trả nợ',     '/loans/repay-plan',               'list',   'loan',    'loan.read',    (SELECT id FROM plt_menus WHERE code = 'admin.loan' AND tenant_id IS NULL), 60)
ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING;

-- +goose Down
DELETE FROM plt_menus WHERE code IN (
    'finance.posting', 'finance.posting_single', 'finance.posting_double',
    'finance.posting_off_balance', 'finance.posting_closing',
    'finance.posting_cancellation', 'finance.statements',
    'loan.adjustments', 'loan.repay_plan'
) AND tenant_id IS NULL;
