-- +goose Up

-- Agreements need a currency for money arithmetic (arda-money rounding).
ALTER TABLE lnm_agreements ADD COLUMN IF NOT EXISTS currency_code VARCHAR(8) NOT NULL DEFAULT 'VND';

-- +goose Down
ALTER TABLE lnm_agreements DROP COLUMN IF EXISTS currency_code;
