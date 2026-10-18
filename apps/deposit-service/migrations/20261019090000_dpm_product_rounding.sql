-- +goose Up

-- DPM interest rounding per product (EPAS dpm_inf_product.ROUND_NO / ROUND_TYPE).
--
-- Daily accrual rounds the cumulative interest of the period to a multiple of
-- round_no with the CM163 mode (T9.1, owner decision of 2026-10-18). The two
-- columns are deliberately NULL with no DEFAULT: the migration does not invent
-- a rounding step. A product without them is skipped by the daily accrual and
-- reported with problem code dpm.accrual.round_config_missing until the values
-- are entered through the product edit request (maker-checker).
--
-- round_no = 0 means "no step": the accrued amount is rounded once to the dong.
-- round_type: CM163.001 HALF_UP, CM163.002 CEILING, CM163.003 FLOOR.

ALTER TABLE dpm_products
    ADD COLUMN IF NOT EXISTS round_no   INTEGER,
    ADD COLUMN IF NOT EXISTS round_type VARCHAR(16);

ALTER TABLE dpm_products
    ADD CONSTRAINT chk_dpm_product_round_no CHECK (round_no IS NULL OR round_no >= 0),
    ADD CONSTRAINT chk_dpm_product_round_type CHECK (round_type IS NULL OR round_type IN ('CM163.001', 'CM163.002', 'CM163.003')),
    ADD CONSTRAINT chk_dpm_product_round_pair CHECK ((round_no IS NULL) = (round_type IS NULL));

COMMENT ON COLUMN dpm_products.round_no IS
    'Rounding step of the cumulative daily accrual (VND); 0 = round once to the dong; NULL = not configured';
COMMENT ON COLUMN dpm_products.round_type IS
    'CM163 rounding mode: CM163.001 HALF_UP, CM163.002 CEILING, CM163.003 FLOOR; NULL = not configured';

ALTER TABLE dpm_product_requests
    ADD COLUMN IF NOT EXISTS round_no   INTEGER,
    ADD COLUMN IF NOT EXISTS round_type VARCHAR(16);

ALTER TABLE dpm_product_requests
    ADD CONSTRAINT chk_dpm_product_request_round_no CHECK (round_no IS NULL OR round_no >= 0),
    ADD CONSTRAINT chk_dpm_product_request_round_type CHECK (round_type IS NULL OR round_type IN ('CM163.001', 'CM163.002', 'CM163.003')),
    ADD CONSTRAINT chk_dpm_product_request_round_pair CHECK ((round_no IS NULL) = (round_type IS NULL));

-- +goose Down

ALTER TABLE dpm_product_requests
    DROP CONSTRAINT IF EXISTS chk_dpm_product_request_round_pair,
    DROP CONSTRAINT IF EXISTS chk_dpm_product_request_round_type,
    DROP CONSTRAINT IF EXISTS chk_dpm_product_request_round_no,
    DROP COLUMN IF EXISTS round_type,
    DROP COLUMN IF EXISTS round_no;

ALTER TABLE dpm_products
    DROP CONSTRAINT IF EXISTS chk_dpm_product_round_pair,
    DROP CONSTRAINT IF EXISTS chk_dpm_product_round_type,
    DROP CONSTRAINT IF EXISTS chk_dpm_product_round_no,
    DROP COLUMN IF EXISTS round_type,
    DROP COLUMN IF EXISTS round_no;
