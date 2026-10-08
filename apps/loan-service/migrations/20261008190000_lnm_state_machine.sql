-- +goose Up

-- Refuse to normalize values this migration has not classified. This keeps a
-- newly introduced status from being silently rewritten during rollout.
-- +goose StatementBegin
DO $$
DECLARE
    table_name TEXT;
    unknown_statuses TEXT;
BEGIN
    FOR table_name IN SELECT unnest(ARRAY[
        'lnm_contracts', 'lnm_agreements', 'lnm_disbursements',
        'lnm_collections', 'lnm_disbursement_batches', 'lnm_collection_batches'
    ]) LOOP
        EXECUTE format(
            'SELECT string_agg(DISTINCT status, '', '' ORDER BY status) FROM %I WHERE status NOT IN (%s)',
            table_name,
            CASE table_name
                WHEN 'lnm_contracts' THEN '''DRAFT'',''PENDING'',''PENDING_APPROVAL'',''APPROVED'',''ACTIVE'',''DISBURSED'',''REJECTED'',''CANCELLED'',''CLOSED'''
                WHEN 'lnm_agreements' THEN '''PENDING'',''ACTIVE'',''DISBURSED'',''CLOSED'''
                ELSE '''DRAFT'',''SUBMITTED'',''PENDING_APPROVAL'',''APPROVED'',''REJECTED'',''CANCELLED'',''POSTED'',''REVERSED'''
            END
        ) INTO unknown_statuses;
        IF unknown_statuses IS NOT NULL THEN
            RAISE EXCEPTION 'unmapped status value(s) in %: %', table_name, unknown_statuses;
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

UPDATE lnm_contracts
SET status = CASE status
    WHEN 'PENDING' THEN 'PENDING_APPROVAL'
    WHEN 'ACTIVE' THEN 'DISBURSED'
    ELSE status
END;
UPDATE lnm_agreements SET status = 'ACTIVE' WHERE status = 'PENDING';
UPDATE lnm_agreements SET status = 'ACTIVE' WHERE status = 'DISBURSED';
UPDATE lnm_disbursements SET status = 'PENDING_APPROVAL' WHERE status = 'SUBMITTED';
UPDATE lnm_collections SET status = 'PENDING_APPROVAL' WHERE status = 'SUBMITTED';
UPDATE lnm_disbursement_batches SET status = 'PENDING_APPROVAL' WHERE status = 'SUBMITTED';
UPDATE lnm_collection_batches SET status = 'PENDING_APPROVAL' WHERE status = 'SUBMITTED';

ALTER TABLE lnm_contracts
    ADD CONSTRAINT chk_lnm_contracts_status
    CHECK (status IN ('DRAFT', 'PENDING_APPROVAL', 'APPROVED', 'REJECTED', 'CANCELLED', 'DISBURSED', 'CLOSED'));
ALTER TABLE lnm_agreements
    ADD CONSTRAINT chk_lnm_agreements_status
    CHECK (status IN ('ACTIVE', 'CLOSED'));

ALTER TABLE lnm_disbursement_batches DROP CONSTRAINT IF EXISTS lnm_disbursement_batches_status_check;
ALTER TABLE lnm_disbursement_batches
    ADD CONSTRAINT chk_lnm_disbursement_batches_status
    CHECK (status IN ('DRAFT', 'PENDING_APPROVAL', 'APPROVED', 'REJECTED', 'CANCELLED', 'POSTED', 'REVERSED'));
ALTER TABLE lnm_collection_batches DROP CONSTRAINT IF EXISTS lnm_collection_batches_status_check;
ALTER TABLE lnm_collection_batches
    ADD CONSTRAINT chk_lnm_collection_batches_status
    CHECK (status IN ('DRAFT', 'PENDING_APPROVAL', 'APPROVED', 'REJECTED', 'CANCELLED', 'POSTED', 'REVERSED'));
ALTER TABLE lnm_disbursements
    ADD CONSTRAINT chk_lnm_disbursements_status
    CHECK (status IN ('DRAFT', 'PENDING_APPROVAL', 'APPROVED', 'REJECTED', 'CANCELLED', 'POSTED', 'REVERSED'));
ALTER TABLE lnm_collections
    ADD CONSTRAINT chk_lnm_collections_status
    CHECK (status IN ('DRAFT', 'PENDING_APPROVAL', 'APPROVED', 'REJECTED', 'CANCELLED', 'POSTED', 'REVERSED'));

ALTER TABLE lnm_repay_plans
    ADD COLUMN lifecycle_status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    ADD COLUMN payment_status VARCHAR(16) GENERATED ALWAYS AS (
        CASE
            WHEN coln_principal_amt_minor + coln_interest_amt_minor = 0 THEN 'PLANNED'
            WHEN coln_principal_amt_minor + coln_interest_amt_minor < plan_principal_amt_minor + plan_interest_amt_minor THEN 'PARTIALLY_PAID'
            ELSE 'PAID'
        END
    ) STORED;
UPDATE lnm_repay_plans
SET lifecycle_status = CASE WHEN is_active THEN 'ACTIVE' ELSE 'SUPERSEDED' END;
ALTER TABLE lnm_repay_plans
    ADD CONSTRAINT chk_lnm_repay_plans_lifecycle_status
        CHECK (lifecycle_status IN ('ACTIVE', 'SUPERSEDED')),
    ADD CONSTRAINT chk_lnm_repay_plans_payment_status
        CHECK (payment_status IN ('PLANNED', 'PARTIALLY_PAID', 'PAID')),
    ADD CONSTRAINT chk_lnm_repay_plans_active_mirror
        CHECK (is_active = (lifecycle_status = 'ACTIVE'));

-- +goose Down
-- Manual rollback: first stop writers that use canonical values. Agreement
-- PENDING and DISBURSED both normalize to ACTIVE, so their original spelling
-- cannot be recovered; leave agreements ACTIVE unless an operator has a
-- business-approved row mapping. Remaining statuses map back below.
ALTER TABLE lnm_repay_plans
    DROP CONSTRAINT IF EXISTS chk_lnm_repay_plans_active_mirror,
    DROP CONSTRAINT IF EXISTS chk_lnm_repay_plans_payment_status,
    DROP CONSTRAINT IF EXISTS chk_lnm_repay_plans_lifecycle_status;
ALTER TABLE lnm_repay_plans DROP COLUMN IF EXISTS payment_status, DROP COLUMN IF EXISTS lifecycle_status;

ALTER TABLE lnm_contracts DROP CONSTRAINT IF EXISTS chk_lnm_contracts_status;
ALTER TABLE lnm_agreements DROP CONSTRAINT IF EXISTS chk_lnm_agreements_status;
ALTER TABLE lnm_disbursements DROP CONSTRAINT IF EXISTS chk_lnm_disbursements_status;
ALTER TABLE lnm_collections DROP CONSTRAINT IF EXISTS chk_lnm_collections_status;
ALTER TABLE lnm_disbursement_batches DROP CONSTRAINT IF EXISTS chk_lnm_disbursement_batches_status;
ALTER TABLE lnm_collection_batches DROP CONSTRAINT IF EXISTS chk_lnm_collection_batches_status;

UPDATE lnm_contracts SET status = CASE status
    WHEN 'PENDING_APPROVAL' THEN 'PENDING'
    WHEN 'DISBURSED' THEN 'ACTIVE'
    ELSE status
END;
UPDATE lnm_disbursements SET status = 'SUBMITTED' WHERE status = 'PENDING_APPROVAL';
UPDATE lnm_collections SET status = 'SUBMITTED' WHERE status = 'PENDING_APPROVAL';
UPDATE lnm_disbursement_batches SET status = 'SUBMITTED' WHERE status = 'PENDING_APPROVAL';
UPDATE lnm_collection_batches SET status = 'SUBMITTED' WHERE status = 'PENDING_APPROVAL';
