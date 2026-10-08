-- +goose Up

-- The old REGISTER settle added each drawdown to both outstanding and
-- pending. Normalize that legacy overlap: pending is the uncompleted part,
-- so it is removed from outstanding before the new disjoint-balance formula
-- (outstanding + pending + HELD reservations) is used.
-- Stop rather than guess if the old balance cannot cover its recorded pending.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM lnm_agreements
        WHERE pending_disburse_amt_minor < 0
           OR outstanding_amt_minor < pending_disburse_amt_minor
    ) THEN
        RAISE EXCEPTION 'cannot normalize loan agreement balances: pending exceeds outstanding';
    END IF;
END $$;
-- +goose StatementEnd

UPDATE lnm_agreements
SET outstanding_amt_minor = outstanding_amt_minor - pending_disburse_amt_minor,
    updated_at = now()
WHERE pending_disburse_amt_minor > 0;

CREATE TABLE lnm_contract_reservations (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     VARCHAR(64) NOT NULL,
    contract_code VARCHAR(64) NOT NULL,
    source_type   VARCHAR(24) NOT NULL,
    source_id     UUID NOT NULL,
    amount_minor  BIGINT NOT NULL CHECK (amount_minor > 0),
    status        VARCHAR(12) NOT NULL CHECK (status IN ('HELD', 'CONSUMED', 'RELEASED')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_type, source_id)
);

CREATE INDEX idx_lnm_contract_reservations_exposure
    ON lnm_contract_reservations (tenant_id, contract_code, status);

-- Preserve in-flight work across the deployment. POSTED rows have already
-- moved into agreement balances; unresolved rows still need a HELD reservation.
INSERT INTO lnm_contract_reservations
    (tenant_id, contract_code, source_type, source_id, amount_minor, status)
SELECT tenant_id, contract_code, 'DISBURSEMENT', id, disburse_amt_minor,
       CASE status
           WHEN 'DRAFT' THEN 'HELD'
           WHEN 'SUBMITTED' THEN 'HELD'
           WHEN 'APPROVED' THEN 'HELD'
           WHEN 'POSTED' THEN 'CONSUMED'
           ELSE 'RELEASED'
       END
FROM lnm_disbursements
WHERE flow_type = 'REGISTER' AND disburse_amt_minor > 0;

-- Existing unresolved requests can reveal a pre-existing over-limit contract.
-- Fail the migration for explicit review instead of hiding or releasing work.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM lnm_contracts c
        WHERE COALESCE((SELECT SUM(a.outstanding_amt_minor) FROM lnm_agreements a
                        WHERE a.tenant_id = c.tenant_id AND a.contract_code = c.contract_code), 0)
            + COALESCE((SELECT SUM(a.pending_disburse_amt_minor) FROM lnm_agreements a
                        WHERE a.tenant_id = c.tenant_id AND a.contract_code = c.contract_code), 0)
            + COALESCE((SELECT SUM(r.amount_minor) FROM lnm_contract_reservations r
                        WHERE r.tenant_id = c.tenant_id AND r.contract_code = c.contract_code
                          AND r.status = 'HELD'), 0) > c.loan_amt_minor
    ) THEN
        RAISE EXCEPTION 'cannot migrate loan contract reservations: existing exposure exceeds contract limit';
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- No down: balance normalization and consumed reservations are durable ledger state.
SELECT 1;
