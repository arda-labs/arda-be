-- +goose Up

-- Fix the seed formulas that wrongly multiplied minor-unit amounts by 100.
--
-- Arda stores money as int64 *_minor per ISO 4217 (docs/db-schema-conventions.md
-- §5). VND has zero decimal digits, so minor == major: 100000000 minor IS
-- 100,000,000 VND. The seed added "scale":100 to every amount indicator, which
-- inflated each value by 100x and made the indicator disagree with the report
-- builder reading the same fact column (DEPOSIT_PORTFOLIO reported
-- principal_minor: 100000000 while 20000.01 claimed 10,000,000,000). The
-- assistant noticed the contradiction during NL-routing verification.
--
-- Removing scale restores the one-unit contract: indicator value == the fact
-- column value the report shows. A currency with real minor units (USD cents)
-- would need an explicit per-currency scale, which is a separate design step —
-- no VND indicator should carry one today.

UPDATE rpt_indicators
SET formula = formula - 'scale',
    updated_at = now(),
    version = version + 1
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND formula ? 'scale';

-- Recompute is the operator's step after this migration (POST
-- /api/statistical/indicators/compute), so stale values are not mistaken for
-- the corrected ones.

-- +goose Down
-- No down: the previous values were wrong by 100x and must not be restored.
SELECT 1;
