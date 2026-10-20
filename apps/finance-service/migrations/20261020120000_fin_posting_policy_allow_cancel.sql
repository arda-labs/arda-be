-- +goose Up

-- Danh mục loại chứng từ được hủy (AP-REV-01, tương đương
-- com_cfg_process_type.is_cancel của EPAS). Cột nằm trên fin_posting_policies
-- (một dòng mỗi tenant + business_doc_type):
--   NULL  = theo mặc định trong mã (PostingService.EnsureReversalAllowed:
--           chỉ chứng từ thủ công FIN_SINGLE_ENTRY/FIN_DOUBLE_ENTRY/
--           FIN_OFF_BALANCE/FIN_FUND_APPROP/FIN_FUND_USE được đảo),
--   TRUE  = bật hủy cho loại FIN_* khác,
--   FALSE = tắt hủy cho loại FIN_* mặc định được hủy.
-- Chứng từ sinh từ nghiệp vụ domain (LNM_*, DEP_*, ...) không bật được: đảo
-- sổ cái ở finance không quay lại sổ chi tiết nghiệp vụ, nên phải hoàn tác
-- qua domain. Ràng buộc CHECK giữ bất biến đó ở tầng DB.
ALTER TABLE fin_posting_policies ADD COLUMN IF NOT EXISTS allow_cancel BOOLEAN;
COMMENT ON COLUMN fin_posting_policies.allow_cancel IS
    'Danh mục loại chứng từ được hủy: NULL = mặc định (chỉ chứng từ thủ công FIN_*), TRUE/FALSE = ghi đè; chỉ loại FIN_* (trừ FIN_TXN_CANCEL) được bật';
ALTER TABLE fin_posting_policies
    ADD CONSTRAINT chk_fin_posting_policies_allow_cancel_fin_only
    CHECK (allow_cancel IS NOT TRUE
           OR (left(business_doc_type, 4) = 'FIN_' AND business_doc_type <> 'FIN_TXN_CANCEL'));

-- +goose Down
ALTER TABLE fin_posting_policies
    DROP CONSTRAINT IF EXISTS chk_fin_posting_policies_allow_cancel_fin_only;
ALTER TABLE fin_posting_policies DROP COLUMN IF EXISTS allow_cancel;
