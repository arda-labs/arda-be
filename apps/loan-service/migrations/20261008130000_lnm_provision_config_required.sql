-- +goose Up

-- Business must explicitly supply collateral deduction configuration. Existing
-- values are preserved; new records must not silently inherit a zero ratio.
ALTER TABLE lnm_collaterals ALTER COLUMN deduction_ratio DROP DEFAULT;

-- +goose Down
ALTER TABLE lnm_collaterals ALTER COLUMN deduction_ratio SET DEFAULT 0;
