-- +goose Up

-- Remap PLI/PLII account codes from the EPAS chart to Arda's real posting
-- accounts (Cách B hybrid): 121->1311, 511->5111/5112, 711->7111,
-- 611->61112/6321/8021/8011, 615->8011/8021, 642/811->8111, 411->4311;
-- 515/821 drop (no Arda equivalent). Fund accounts (418xx/353xx) stay.

UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["41801"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.002';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["41802"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.003';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["35302","35303"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.004';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["35304","35305"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.005';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["35302"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.006';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["41803"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.007';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41801"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.010';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41802"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.011';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35302","35303"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.012';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35304","35305"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.013';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35302"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.014';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41803"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.015';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41801"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.018';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41802"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.019';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35302","35303"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.020';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35304","35305"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.021';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35302"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.022';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41803"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_B' AND row_code = 'PLI_B.023';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["41801"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.002';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["41802"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.003';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["35302","35303"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.004';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["35304","35305"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.005';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["35302"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.006';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["41803"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.007';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41801"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.010';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41802"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.011';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35302","35303"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.012';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35304","35305"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.013';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35302"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.014';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41803"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.015';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41801"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.018';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41802"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.019';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35302","35303"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.020';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35304","35305"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.021';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["35302"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.022';
UPDATE fin_statement_formula SET formula = '{"type":"movement","codes":["41803"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLI_C' AND row_code = 'PLI_C.023';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["4311"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIb' AND row_code = 'PLIIb.002';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["418"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIb' AND row_code = 'PLIIb.004';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["418"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIb' AND row_code = 'PLIIb.005';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["4311"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIb' AND row_code = 'PLIIb.006';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["1311"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIb' AND row_code = 'PLIIb.008';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["1311"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIb' AND row_code = 'PLIIb.010';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["5111","5112","7111"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIb' AND row_code = 'PLIIb.014';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["61112","6321","8021","8011","8111"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIb' AND row_code = 'PLIIb.015';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["4311"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIc' AND row_code = 'PLIIc.002';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["418"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIc' AND row_code = 'PLIIc.004';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["418"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIc' AND row_code = 'PLIIc.005';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["4311"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIc' AND row_code = 'PLIIc.006';
UPDATE fin_statement_formula SET formula = '{"type":"opening","codes":["1311"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIc' AND row_code = 'PLIIc.008';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["1311"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIc' AND row_code = 'PLIIc.009';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["5111","5112","7111"],"side":"CREDIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIc' AND row_code = 'PLIIc.011';
UPDATE fin_statement_formula SET formula = '{"type":"accounts","codes":["61112","6321","8021","8011","8111"],"side":"DEBIT","prefix":true}', updated_at = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND statement_code = 'PLIIc' AND row_code = 'PLIIc.012';

-- +goose Down
SELECT 1;
