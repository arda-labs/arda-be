-- +goose Up

-- Customer fact flags (PCF topic "Khách hàng"). The catalog's customer counts
-- are not just "all customers": it counts members vs non-members, customers
-- with a deposit balance, customers with a loan, and so on. Those are
-- cross-fact predicates over facts the ETL already loads, so the ETL stamps
-- them onto the customer row after all sources land.
--
-- Flags are VARCHAR(1) 'Y'/'N' rather than BOOLEAN because the indicator engine
-- compares bound text literals; a boolean would need a cast in every filter.

ALTER TABLE rpt_fact_customer_daily
    ADD COLUMN IF NOT EXISTS is_member   VARCHAR(1) NOT NULL DEFAULT 'N',
    ADD COLUMN IF NOT EXISTS has_deposit VARCHAR(1) NOT NULL DEFAULT 'N',
    ADD COLUMN IF NOT EXISTS has_loan    VARCHAR(1) NOT NULL DEFAULT 'N';

CREATE INDEX IF NOT EXISTS idx_rpt_fact_customer_member
    ON rpt_fact_customer_daily (tenant_id, business_date, is_member);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_customer_deposit
    ON rpt_fact_customer_daily (tenant_id, business_date, has_deposit);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_customer_loan
    ON rpt_fact_customer_daily (tenant_id, business_date, has_loan);

-- +goose Down
DROP INDEX IF EXISTS idx_rpt_fact_customer_loan;
DROP INDEX IF EXISTS idx_rpt_fact_customer_deposit;
DROP INDEX IF EXISTS idx_rpt_fact_customer_member;
ALTER TABLE rpt_fact_customer_daily
    DROP COLUMN IF EXISTS has_loan,
    DROP COLUMN IF EXISTS has_deposit,
    DROP COLUMN IF EXISTS is_member;
