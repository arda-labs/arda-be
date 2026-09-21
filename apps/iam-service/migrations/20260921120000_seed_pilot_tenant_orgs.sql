-- +goose Up

-- Pilot tenant seeded from the two most-used EPAS organizations (W3).
-- EPAS org codes are kept verbatim so legacy IDs stay traceable.
--   tenant: 00000000-0000-0000-0000-000000000020 (code ngv-pilot)
--   org 01: Hội sở chính (HQ)
--   org 02: Hà Nội
-- Users/role memberships are seeded later (need real accounts); this migration
-- is additive and idempotent.

INSERT INTO iam_tenants (id, code, name, status)
VALUES ('00000000-0000-0000-0000-000000000020', 'ngv-pilot', 'NGV Pilot (EPAS org 01/02)', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET
    code = EXCLUDED.code,
    name = EXCLUDED.name,
    status = EXCLUDED.status,
    updated_at = now();

INSERT INTO iam_organizations (code, name, status, tenant_id)
VALUES
    ('01', 'Hội sở chính', 'ACTIVE', '00000000-0000-0000-0000-000000000020'),
    ('02', 'Hà Nội', 'ACTIVE', '00000000-0000-0000-0000-000000000020')
ON CONFLICT (tenant_id, code) DO UPDATE SET
    name = EXCLUDED.name,
    status = EXCLUDED.status,
    updated_at = now();

-- +goose Down

DELETE FROM iam_organizations
WHERE tenant_id = '00000000-0000-0000-0000-000000000020'
  AND code IN ('01', '02');

DELETE FROM iam_tenants
WHERE id = '00000000-0000-0000-0000-000000000020';
