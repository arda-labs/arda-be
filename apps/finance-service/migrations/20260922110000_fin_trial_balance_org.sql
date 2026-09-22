-- +goose Up

-- P3 reporting data layer (arda-be/docs/reporting-data-layer.md): the daily
-- trial-balance anchor gains the org dimension the QCMS/accounting reports
-- (307 of the 923 indicators) need. EPAS carried org_code + acc_scope +
-- period_acct on fac_inf_trial_balance; Arda adds org_code only.
--
-- org_code is sourced from fin_journal_entries.metadata->>'org_code', which the
-- PostingService stamps from the caller's verified org context (x-org-id /
-- x-user-org-ids). Opening-balance rows carry no org ('' = tenant-wide), and
-- legacy entries posted before the stamp existed keep '' — never a synthetic
-- default. The rebuild key therefore extends to
-- (tenant, business_date, org_code, coa_version, account_code, currency_code).

ALTER TABLE fin_trial_balance_daily
    ADD COLUMN IF NOT EXISTS org_code VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE fin_trial_balance_daily
    DROP CONSTRAINT IF EXISTS fin_trial_balance_daily_pkey;

ALTER TABLE fin_trial_balance_daily
    ADD PRIMARY KEY (tenant_id, business_date, org_code, coa_version, account_code, currency_code);

CREATE INDEX IF NOT EXISTS idx_fin_tbd_org
    ON fin_trial_balance_daily (tenant_id, org_code, business_date DESC);

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
