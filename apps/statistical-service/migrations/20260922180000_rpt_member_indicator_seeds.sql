-- +goose Up

-- Seed the QTDND membership indicators that Arda can now resolve (step 6):
-- the stock indicators (member counts, capital) and the pipeline indicators
-- (pending/approved capital requests). Codes come from the PCF catalog
-- (docs/epas-survey/exports/pcf-kpi-catalog.json, topic "Góp vốn cổ phần").
--
-- Amounts follow the fact columns 1:1 (minor units, no scale) — see the
-- 20260922140000 scale fix. Ratios/growth that need a previous period stay out
-- until their base indicator is seeded and computed.

INSERT INTO rpt_indicators
    (tenant_id, code, name, unit, group_code, created_by,
     kpi_type, periodicity, is_ratio, root_code, root_name, meaning,
     sources, formula, dimensions, display_format, rounding_digits)
VALUES
    ('00000000-0000-0000-0000-000000000010', '10021.02', 'Số lượng thành viên', 'thành viên', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10021', 'Số lượng thành viên',
     'Tổng số thành viên đang hoạt động tại Quỹ',
     '["rpt_fact_member_daily"]',
     '{"type":"count","fact":"rpt_fact_member_daily","filter":{"member_status":"ACTIVE"},"as_of":"period_end"}',
     '["org","member_type","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10021.04', 'Số lượng thành viên là cá nhân', 'thành viên', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10021', 'Số lượng thành viên',
     'Số thành viên là cá nhân',
     '["rpt_fact_member_daily"]',
     '{"type":"count","fact":"rpt_fact_member_daily","filter":{"member_status":"ACTIVE","member_type_code":"INDIVIDUAL"},"as_of":"period_end"}',
     '["org","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10021.05', 'Số lượng thành viên là pháp nhân', 'thành viên', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10021', 'Số lượng thành viên',
     'Số thành viên là pháp nhân',
     '["rpt_fact_member_daily"]',
     '{"type":"count","fact":"rpt_fact_member_daily","filter":{"member_status":"ACTIVE","member_type_code":"LEGAL"},"as_of":"period_end"}',
     '["org","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10021.06', 'Số lượng thành viên là hộ gia đình', 'thành viên', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10021', 'Số lượng thành viên',
     'Số thành viên thuộc hộ gia đình',
     '["rpt_fact_member_daily"]',
     '{"type":"count","fact":"rpt_fact_member_daily","filter":{"member_status":"ACTIVE","member_type_code":"HOUSEHOLD"},"as_of":"period_end"}',
     '["org","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10022.01', 'Số lượng thành viên góp vốn đã duyệt', 'thành viên', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10022', 'Số lượng thành viên góp vốn đã duyệt',
     'Số thành viên có vốn góp đã được duyệt',
     '["rpt_fact_member_daily"]',
     '{"type":"count","fact":"rpt_fact_member_daily","filter":{"member_status":"ACTIVE","total_capital_minor":{"op":">","value":"0"}},"as_of":"period_end"}',
     '["org","member_type","time"]', 'int', 0),
    ('00000000-0000-0000-0000-000000000010', '10024.04', 'Tổng vốn góp cổ phần của thành viên', 'VND', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10024', 'Vốn góp cổ phần',
     'Tổng số tiền vốn góp cổ phần của thành viên',
     '["rpt_fact_member_daily"]',
     '{"type":"sum","fact":"rpt_fact_member_daily","column":"total_capital_minor","filter":{"member_status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","member_type","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '10025.01', 'Vốn góp xác lập', 'VND', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10025', 'Vốn góp của thành viên theo loại cổ phần',
     'Tổng vốn góp xác lập tư cách thành viên đã duyệt',
     '["rpt_fact_member_daily"]',
     '{"type":"sum","fact":"rpt_fact_member_daily","column":"estb_capital_minor","filter":{"member_status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '10025.02', 'Vốn góp bổ sung', 'VND', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10025', 'Vốn góp của thành viên theo loại cổ phần',
     'Tổng vốn góp bổ sung đã duyệt',
     '["rpt_fact_member_daily"]',
     '{"type":"sum","fact":"rpt_fact_member_daily","column":"add_capital_minor","filter":{"member_status":"ACTIVE"},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '10030.01', 'Vốn góp xác lập chờ duyệt', 'VND', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10030', 'Vốn góp theo hình thức duyệt cổ phần',
     'Tổng vốn góp xác lập đang chờ duyệt',
     '["rpt_fact_member_request_daily"]',
     '{"type":"sum","fact":"rpt_fact_member_request_daily","column":"amount_minor","filter":{"status":["DRAFT","SUBMITTED"],"request_type":"REGISTER"},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '10030.02', 'Vốn góp xác lập đã duyệt', 'VND', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10030', 'Vốn góp theo hình thức duyệt cổ phần',
     'Tổng vốn góp xác lập đã được duyệt',
     '["rpt_fact_member_request_daily"]',
     '{"type":"sum","fact":"rpt_fact_member_request_daily","column":"amount_minor","filter":{"status":"APPROVED","request_type":"REGISTER"},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '10030.03', 'Vốn góp bổ sung chờ duyệt', 'VND', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10030', 'Vốn góp theo hình thức duyệt cổ phần',
     'Tổng vốn góp bổ sung đang chờ duyệt',
     '["rpt_fact_member_request_daily"]',
     '{"type":"sum","fact":"rpt_fact_member_request_daily","column":"amount_minor","filter":{"status":["DRAFT","SUBMITTED"],"request_type":"ADDITIONAL"},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2),
    ('00000000-0000-0000-0000-000000000010', '10030.04', 'Vốn góp bổ sung đã duyệt', 'VND', 'Góp vốn cổ phần', 'seed',
     'P', 'D', false, '10030', 'Vốn góp theo hình thức duyệt cổ phần',
     'Tổng vốn góp bổ sung đã được duyệt',
     '["rpt_fact_member_request_daily"]',
     '{"type":"sum","fact":"rpt_fact_member_request_daily","column":"amount_minor","filter":{"status":"APPROVED","request_type":"ADDITIONAL"},"as_of":"period_end","scale":1}',
     '["org","time"]', 'amount', 2)
ON CONFLICT (tenant_id, code) DO UPDATE SET
    name = EXCLUDED.name, unit = EXCLUDED.unit, group_code = EXCLUDED.group_code,
    kpi_type = EXCLUDED.kpi_type, periodicity = EXCLUDED.periodicity,
    is_ratio = EXCLUDED.is_ratio, root_code = EXCLUDED.root_code, root_name = EXCLUDED.root_name,
    meaning = EXCLUDED.meaning, sources = EXCLUDED.sources, formula = EXCLUDED.formula,
    dimensions = EXCLUDED.dimensions, display_format = EXCLUDED.display_format,
    rounding_digits = EXCLUDED.rounding_digits, updated_at = now();

-- +goose Down
DELETE FROM rpt_indicators
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND code IN ('10021.02','10021.04','10021.05','10021.06','10022.01','10024.04',
               '10025.01','10025.02','10030.01','10030.02','10030.03','10030.04');
