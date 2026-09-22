-- +goose Up

-- Fix the two series indicators seeded in 20260922191000: growth and
-- trailing_average resolve the BASE INDICATOR by code through ComputeSeries,
-- not the fact/column directly. Without an `indicator` the growth formula fails
-- "growth requires an indicator code" and the rolling average silently returns
-- null (empty series).
--
-- 60000.01.02 — growth of 60000.01 (total interbank deposits) vs previous period.
-- 60000.01.01 — 3-month average of 60000.01.

UPDATE rpt_indicators
SET formula = '{"type":"growth","indicator":"60000.01","compare":"previous_period","as_of":"period_end","scale":1}',
    updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND code = '60000.01.02';

UPDATE rpt_indicators
SET formula = '{"type":"trailing_average","indicator":"60000.01","periods":3,"as_of":"period_end","scale":1}',
    updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND code = '60000.01.01';

-- +goose Down

UPDATE rpt_indicators
SET formula = '{"type":"growth","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}',
    updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND code = '60000.01.02';

UPDATE rpt_indicators
SET formula = '{"type":"trailing_average","fact":"rpt_fact_ibm_deposit_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1,"periods":3}',
    updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND code = '60000.01.01';
