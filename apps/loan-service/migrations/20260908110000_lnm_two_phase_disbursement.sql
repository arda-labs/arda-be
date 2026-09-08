-- +goose Up

-- P1b v2 / Wave 3: split the drawdown into the EPAS LNM.300.02 two-flow
-- pattern (REGISTER reserves the in-transit leg, COMPLETE settles cash),
-- riding the finance two-phase posting lifecycle. The agreement carries the
-- pending (in-transit) disbursement balance between the two legs.

ALTER TABLE lnm_agreements
    ADD COLUMN IF NOT EXISTS pending_disburse_amt_minor BIGINT NOT NULL DEFAULT 0;

ALTER TABLE lnm_disbursements
    ADD COLUMN IF NOT EXISTS flow_type VARCHAR(10) NOT NULL DEFAULT 'REGISTER'
    CHECK (flow_type IN ('REGISTER', 'COMPLETE'));

ALTER TABLE lnm_disbursements
    ADD COLUMN IF NOT EXISTS source_register_id UUID REFERENCES lnm_disbursements (id);

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
