-- +goose Up

-- Iteration 12: workflow case code on the loan create paths. The Submit
-- services create the LOAN_FORMATION_V2 / LNM_DISB_*_V2 / LNM_COLLECTION_V2
-- cases and now persist the human-readable case code next to the case uuid —
-- the FE list/detail shows the code instead of the raw uuid.

ALTER TABLE lnm_contracts     ADD COLUMN IF NOT EXISTS workflow_case_code TEXT;
ALTER TABLE lnm_disbursements ADD COLUMN IF NOT EXISTS workflow_case_code TEXT;
ALTER TABLE lnm_collections   ADD COLUMN IF NOT EXISTS workflow_case_code TEXT;

-- +goose Down
SELECT 1;
