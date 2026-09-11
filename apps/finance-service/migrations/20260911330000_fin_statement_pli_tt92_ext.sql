-- +goose Up

-- TT92 PLIIb external rows: finance-service resolves these from loan-service
-- via gRPC (GetOperationMetrics) — the loan DB is not readable from finance.
--   .009 Thu nợ trong kỳ  -> loan.collection_volume (Σ collections in period)
--   .017 Dư nợ xấu        -> loan.npl_balance       (outstanding, groups 3-5)
-- .011 (tỷ lệ nợ xấu) is a rows-formula over .017/.010 and needs no change.

UPDATE fin_statement_formula
SET formula = '{"type":"external","dataset":"loan.collection_volume"}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND statement_code = 'PLIIb' AND row_code = 'PLIIb.009';

UPDATE fin_statement_formula
SET formula = '{"type":"external","dataset":"loan.npl_balance"}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND statement_code = 'PLIIb' AND row_code = 'PLIIb.017';

-- +goose Down

UPDATE fin_statement_formula
SET formula = '{"type":"none"}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND statement_code = 'PLIIb' AND row_code IN ('PLIIb.009', 'PLIIb.017');
