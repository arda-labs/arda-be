-- +goose Up

-- PLIIb.011 (tỷ lệ nợ xấu): EPAS's summary engine ignores the "/" token, so its
-- effective value equals PLIIb.017 (nợ xấu). Arda evaluates rows in sort order
-- (single pass) and .011 precedes .017, so referencing the row would yield 0;
-- bind it to the same external dataset instead (numeric parity with EPAS).

UPDATE fin_statement_formula
SET formula = '{"type":"external","dataset":"loan.npl_balance"}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND statement_code = 'PLIIb' AND row_code = 'PLIIb.011';

-- +goose Down

UPDATE fin_statement_formula
SET formula = '{"type":"rows","members":[{"code":"PLIIb.017"},{"code":"/PLIIb.010"}]}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND statement_code = 'PLIIb' AND row_code = 'PLIIb.011';
