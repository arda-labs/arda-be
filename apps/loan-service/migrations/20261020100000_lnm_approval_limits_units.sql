-- +goose Up

-- AP-APR-01: sửa đơn vị seed hạn mức phê duyệt và chặn cấu hình sai ở DB.
--
-- 20260909103000_lnm_approval_limits.sql chú thích đúng ý định (500 triệu /
-- 2 tỷ VND) nhưng ghi giá trị theo số mũ 2 (xu) trong khi VND có số mũ 0
-- (arda-money: "VND": 0 → amount_minor là đồng). Kết quả seed thực tế là
-- 50 tỷ / 200 tỷ, lệch 100 lần. Migration cũ giữ nguyên (không sửa migration
-- đã chạy); dòng pilot được chỉnh tại đây, chỉ khi còn đúng giá trị sai cũ để
-- không ghi đè hạn mức đã được cấu hình lại bằng tay.
--
-- Đơn vị (VND, số mũ 0, amount_minor = đồng):
--   pgd_limit_minor = 500_000_000   = 500 triệu VND (Phòng giao dịch)
--   gd_limit_minor  = 2_000_000_000 = 2 tỷ VND      (Giám đốc)
--
-- scripts/check-approval-limits.mjs đối chiếu mọi giá trị hạn mức seed với
-- chú thích đơn vị ở trên.

UPDATE lnm_approval_limits
SET pgd_limit_minor = 500000000,
    gd_limit_minor  = 2000000000,
    updated_at      = now()
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND org_code = 'LNM_PGD'
  AND product_code = ''
  AND pgd_limit_minor = 50000000000
  AND gd_limit_minor = 200000000000;

-- Cấu hình sai (hạn mức không dương, hoặc hạn mức GĐ nhỏ hơn hạn mức PGD)
-- không được ghi vào bảng; tầng service (ResolveApprovalLimits) cũng chặn
-- nộp hồ sơ khi gặp dòng như vậy.
ALTER TABLE lnm_approval_limits
    DROP CONSTRAINT IF EXISTS chk_lnm_approval_limits_tiers;
ALTER TABLE lnm_approval_limits
    ADD CONSTRAINT chk_lnm_approval_limits_tiers
    CHECK (pgd_limit_minor > 0 AND gd_limit_minor >= pgd_limit_minor);

-- +goose Down
ALTER TABLE lnm_approval_limits
    DROP CONSTRAINT IF EXISTS chk_lnm_approval_limits_tiers;
