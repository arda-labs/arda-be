-- +goose Up

-- P1c.3 data-scope: org_code on loan transaction tables (values come from
-- the X-Org-Id active-org header; list queries filter by X-User-Org-Ids).

ALTER TABLE lnm_contracts    ADD COLUMN IF NOT EXISTS org_code VARCHAR(64);
ALTER TABLE lnm_agreements   ADD COLUMN IF NOT EXISTS org_code VARCHAR(64);
ALTER TABLE lnm_disbursements ADD COLUMN IF NOT EXISTS org_code VARCHAR(64);
ALTER TABLE lnm_collections   ADD COLUMN IF NOT EXISTS org_code VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_lnm_contracts_org  ON lnm_contracts (tenant_id, org_code);
CREATE INDEX IF NOT EXISTS idx_lnm_agreements_org ON lnm_agreements (tenant_id, org_code);
CREATE INDEX IF NOT EXISTS idx_lnm_disb_org       ON lnm_disbursements (tenant_id, org_code);
CREATE INDEX IF NOT EXISTS idx_lnm_col_org        ON lnm_collections (tenant_id, org_code);

-- +goose Down
SELECT 1;
