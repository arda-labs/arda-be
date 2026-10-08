-- +goose Up

ALTER TABLE lnm_disbursement_batches
    DROP CONSTRAINT IF EXISTS chk_lnm_disbursement_batches_status;
ALTER TABLE lnm_disbursement_batches
    ADD CONSTRAINT chk_lnm_disbursement_batches_status
    CHECK (status IN ('DRAFT', 'PENDING_APPROVAL', 'SUBMIT_FAILED', 'APPROVED', 'REJECTED', 'CANCELLED', 'POSTED', 'REVERSED'));

CREATE TABLE lnm_disbursement_workflow_outbox (
    id                 UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          VARCHAR(64) NOT NULL,
    batch_id           UUID NOT NULL REFERENCES lnm_disbursement_batches(id),
    status             VARCHAR(16) NOT NULL DEFAULT 'PENDING'
                       CHECK (status IN ('PENDING', 'PROCESSING', 'DELIVERED', 'DEAD')),
    attempt_count      INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at          TIMESTAMPTZ,
    last_error         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at       TIMESTAMPTZ,
    UNIQUE (tenant_id, batch_id)
);

CREATE INDEX idx_lnm_disb_workflow_outbox_pending
    ON lnm_disbursement_workflow_outbox (next_attempt_at, created_at)
    WHERE status IN ('PENDING', 'PROCESSING');

CREATE TABLE lnm_disbursement_batch_history (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     VARCHAR(64) NOT NULL,
    batch_id      UUID NOT NULL REFERENCES lnm_disbursement_batches(id),
    event_type    VARCHAR(32) NOT NULL,
    from_status   VARCHAR(16) NOT NULL DEFAULT '',
    to_status     VARCHAR(16) NOT NULL DEFAULT '',
    detail        TEXT NOT NULL DEFAULT '',
    actor         VARCHAR(64) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lnm_disb_batch_history
    ON lnm_disbursement_batch_history (tenant_id, batch_id, created_at, id);

-- +goose Down
-- Do not remove retry/history evidence or make SUBMIT_FAILED rows invalid.
-- An operator must first resolve all failed submissions and archive the audit.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM lnm_disbursement_batches WHERE status = 'SUBMIT_FAILED')
       OR EXISTS (SELECT 1 FROM lnm_disbursement_workflow_outbox WHERE status <> 'DELIVERED')
       OR EXISTS (SELECT 1 FROM lnm_disbursement_batch_history) THEN
        RAISE EXCEPTION 'cannot roll back disbursement workflow outbox while submission or history rows exist';
    END IF;
END $$;
-- +goose StatementEnd

DROP TABLE lnm_disbursement_batch_history;
DROP TABLE lnm_disbursement_workflow_outbox;
ALTER TABLE lnm_disbursement_batches
    DROP CONSTRAINT IF EXISTS chk_lnm_disbursement_batches_status;
ALTER TABLE lnm_disbursement_batches
    ADD CONSTRAINT chk_lnm_disbursement_batches_status
    CHECK (status IN ('DRAFT', 'PENDING_APPROVAL', 'APPROVED', 'REJECTED', 'CANCELLED', 'POSTED', 'REVERSED'));
