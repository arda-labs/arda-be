-- +goose Up

-- Fix: cfc_contracts.fund_type_code phải tham chiếu business key
-- (tenant_id, code) — API/FE gửi mã loại vốn ("TW"), không phải surrogate id.
-- NOT VALID để không chặn dữ liệu cũ (bảng trước đây chưa dùng được vì FK sai);
-- các row mới vẫn được enforce.

ALTER TABLE cfc_contracts DROP CONSTRAINT IF EXISTS cfc_contracts_fund_type_code_fkey;

ALTER TABLE cfc_contracts
    ADD CONSTRAINT cfc_contracts_fund_type_fk
    FOREIGN KEY (tenant_id, fund_type_code)
    REFERENCES cfc_fund_types (tenant_id, code)
    NOT VALID;

-- +goose Down

ALTER TABLE cfc_contracts DROP CONSTRAINT IF EXISTS cfc_contracts_fund_type_fk;

ALTER TABLE cfc_contracts
    ADD CONSTRAINT cfc_contracts_fund_type_code_fkey
    FOREIGN KEY (fund_type_code) REFERENCES cfc_fund_types (id);
