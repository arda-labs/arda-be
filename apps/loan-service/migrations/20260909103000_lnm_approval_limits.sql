-- +goose Up

-- Iteration 12: approval-tier limits for the LOAN_FORMATION_V2 BPMN
-- GW_ApprovalLevel conditions (amount > pgdLimit / amount > gdLimit). Kept in
-- loan-service on purpose — a platform-params lookup would add a new
-- cross-service dependency for one row; org/product scoping is enough.
--
-- Amounts are int64 MINOR units (amount_minor, VND exponent 0 → đồng):
--   pgd_limit_minor = 50_000_000_000   = 500 triệu VND (Phòng giao dịch tier)
--   gd_limit_minor  = 200_000_000_000  = 2 tỷ VND      (Giám đốc tier)
--
-- product_code '' = org-wide fallback row; the exact product row wins
-- (PickApprovalLimit precedence: product > org > sentinel).

CREATE TABLE IF NOT EXISTS lnm_approval_limits (
    tenant_id        VARCHAR(64) NOT NULL,
    org_code         VARCHAR(32) NOT NULL,
    product_code     VARCHAR(32) NOT NULL DEFAULT '',
    pgd_limit_minor  BIGINT NOT NULL,
    gd_limit_minor   BIGINT NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, org_code, product_code)
);

-- Pilot org row. No plt_organizations seed exists anywhere yet (verified:
-- lnm_contracts.org_code is stamped from X-Org-Id at runtime and no contract
-- seed sets it), so this uses the loan-domain org prefix the workflow
-- formation seeds already use (LNM_* groups). Re-align the code with real
-- platform org data when it lands.
INSERT INTO lnm_approval_limits (tenant_id, org_code, product_code, pgd_limit_minor, gd_limit_minor)
VALUES ('00000000-0000-0000-0000-000000000010', 'LNM_PGD', '', 50000000000, 200000000000)
ON CONFLICT (tenant_id, org_code, product_code) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
