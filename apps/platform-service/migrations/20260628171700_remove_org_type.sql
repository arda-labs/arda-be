-- +goose Up
ALTER TABLE plt_organizations DROP CONSTRAINT IF EXISTS chk_plt_organizations_type;
ALTER TABLE plt_organizations DROP COLUMN IF EXISTS org_type;

-- +goose Down
ALTER TABLE plt_organizations ADD COLUMN org_type VARCHAR(32) NOT NULL DEFAULT 'company';
ALTER TABLE plt_organizations ADD CONSTRAINT chk_plt_organizations_type CHECK (org_type IN ('company', 'legal_entity', 'division', 'department', 'branch', 'team'));
