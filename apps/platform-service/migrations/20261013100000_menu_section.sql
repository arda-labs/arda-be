-- +goose Up

-- Group the sidebar by department (design review 2026-10-13). The section is a
-- machine code; the frontend owns labels and order. Children inherit the
-- section of their root, so only top-level rows carry one.
ALTER TABLE plt_menus ADD COLUMN section VARCHAR(50) NOT NULL DEFAULT '';

-- Credit is a business department, not system administration: lift the loan
-- tree out of "admin" so it can head its own section. Tenant overrides of the
-- same code move with it so the effective tree stays consistent.
UPDATE plt_menus
SET parent_id = NULL, sort_order = 15, updated_at = now()
WHERE code = 'admin.loan';

UPDATE plt_menus SET section = 'credit', updated_at = now()
WHERE code IN ('admin.loan');

UPDATE plt_menus SET section = 'funding', updated_at = now()
WHERE code IN ('deposit', 'capital');

UPDATE plt_menus SET section = 'accounting', updated_at = now()
WHERE code IN ('finance');

UPDATE plt_menus SET section = 'control', updated_at = now()
WHERE code IN ('statistical');

UPDATE plt_menus SET section = 'hr', updated_at = now()
WHERE code IN ('hrm');

UPDATE plt_menus SET section = 'shared', updated_at = now()
WHERE code IN ('customers', 'workflow', 'workbench', 'ai_center');

UPDATE plt_menus SET section = 'admin', updated_at = now()
WHERE code IN ('admin');

-- +goose Down

UPDATE plt_menus
SET parent_id = (
        SELECT p.id FROM plt_menus p
        WHERE p.code = 'admin' AND p.tenant_id IS NULL
    ),
    sort_order = 110,
    updated_at = now()
WHERE code = 'admin.loan' AND tenant_id IS NULL;

ALTER TABLE plt_menus DROP COLUMN section;
