-- +goose Up

-- Carry the contract attributes the PCF credit indicators slice on into the
-- loan fact. The agreement is the measurement grain (one drawdown), but the
-- term, method and industry live on the contract, so the ETL resolves them
-- through the agreement -> contract join.
--
-- Unlocks roughly 90 "theo kỳ hạn" indicators, 23 "theo phương thức" and the
-- industry splits, which today cannot be expressed because the fact only knew
-- agreement/customer/product/org.
--
--   loan_term_months   term normalised to whole months (DAY/30, YEAR*12), so a
--                      bucket boundary is a plain numeric comparison
--   term_bucket        DEMAND (0) | SHORT (<= 12) | MEDIUM_LONG (> 12)
--   loan_method_code   từng lần / hạn mức / cầm cố
--   industry_code      ngành (nông nghiệp / phi nông nghiệp …)
--   purpose_code       mục đích vay

ALTER TABLE rpt_fact_loan_agreement_daily
    ADD COLUMN IF NOT EXISTS loan_term_months INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS term_bucket      VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS loan_method_code VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS industry_code    VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS purpose_code     VARCHAR(32) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_rpt_fact_loan_term_bucket
    ON rpt_fact_loan_agreement_daily (tenant_id, business_date, term_bucket);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_loan_method
    ON rpt_fact_loan_agreement_daily (tenant_id, business_date, loan_method_code);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_loan_industry
    ON rpt_fact_loan_agreement_daily (tenant_id, business_date, industry_code);

-- +goose Down
DROP INDEX IF EXISTS idx_rpt_fact_loan_industry;
DROP INDEX IF EXISTS idx_rpt_fact_loan_method;
DROP INDEX IF EXISTS idx_rpt_fact_loan_term_bucket;
ALTER TABLE rpt_fact_loan_agreement_daily
    DROP COLUMN IF EXISTS purpose_code,
    DROP COLUMN IF EXISTS industry_code,
    DROP COLUMN IF EXISTS loan_method_code,
    DROP COLUMN IF EXISTS term_bucket,
    DROP COLUMN IF EXISTS loan_term_months;
