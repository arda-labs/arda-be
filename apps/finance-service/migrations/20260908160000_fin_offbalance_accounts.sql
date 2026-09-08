-- +goose Up

-- Iteration 10: off-balance accounts (nhập xuất ngoại bảng) for the pilot
-- COA. Acc 091/092 are the memo (ngoai bang) pair — acc_nature 'B' exempts
-- them from the availability checks in balance_math.go (checkReserve /
-- checkPostActual return nil for nature B). Same pilot tenant + V1 version
-- as 20260907090300_pilot_coa_seed.sql.

INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature, is_postable, effective_date)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'V1', '091', 'Tai san ngoai bang', 'ASSET',   'B', TRUE, '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '092', 'Nguon ngoai bang',    'ASSET',   'B', TRUE, '2026-01-01')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

-- Column comment widen: acc_nature now also carries 'B' (off-balance memo),
-- on top of the existing DEBIT/CREDIT convention. Separate ALTER so the
-- original migration file stays untouched (goose checksum).
COMMENT ON COLUMN fin_coa_accounts.acc_nature IS 'DEBIT | CREDIT | B (off-balance memo)';

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
