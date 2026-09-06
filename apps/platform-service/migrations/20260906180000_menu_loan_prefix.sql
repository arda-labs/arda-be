-- +goose Up

-- Loan remote now owns its own top-level prefix (/loans) matching the
-- 1-remote-per-domain convention (finance, hrm, crm...).

UPDATE plt_menus
SET path = '/loans', remote = 'loan', updated_at = now()
WHERE code = 'admin.loan' AND tenant_id IS NULL;

-- +goose Down
UPDATE plt_menus
SET path = '/admin/loan', remote = 'platform', updated_at = now()
WHERE code = 'admin.loan' AND tenant_id IS NULL;
