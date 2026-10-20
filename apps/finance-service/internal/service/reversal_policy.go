package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrReversalNotAllowed: loại chứng từ không nằm trong danh mục được hủy.
// Map sang POSTING_ERROR_CODE_REVERSAL_NOT_ALLOWED (ClassifyPostingError).
var ErrReversalNotAllowed = errors.New("REVERSAL_NOT_ALLOWED")

// reversalDomainFinance là business_domain của chứng từ thủ công lập trong
// finance (manual_posting_workers, fund/closing case service).
const reversalDomainFinance = "fin"

// defaultCancellableDocTypes là danh mục mặc định loại chứng từ được hủy
// (tương đương com_cfg_process_type.is_cancel = 1 của EPAS): chỉ chứng từ
// thủ công FIN_* chỉ tác động sổ cái. FIN_CLOSING (kết chuyển) và
// FIN_TXN_CANCEL (chính bút toán đảo) không nằm trong mặc định.
var defaultCancellableDocTypes = map[string]struct{}{
	"FIN_SINGLE_ENTRY": {},
	"FIN_DOUBLE_ENTRY": {},
	"FIN_OFF_BALANCE":  {},
	"FIN_FUND_APPROP":  {},
	"FIN_FUND_USE":     {},
}

// reversalAllowed là lõi quyết định thuần (AP-REV-01). Chứng từ sinh từ
// nghiệp vụ domain (lnm/dpm/cap/accrual...) luôn bị từ chối: đảo sổ cái ở
// finance không quay lại sổ chi tiết (khế ước, sổ tiết kiệm, kỳ dự thu,
// quỹ), nên phải hoàn tác qua domain. override là cờ allow_cancel của
// fin_posting_policies (nil = theo mặc định); ràng buộc DB chỉ cho bật cờ
// trên loại FIN_*.
func reversalAllowed(domain, docType string, override *bool) bool {
	if domain != reversalDomainFinance || !strings.HasPrefix(docType, "FIN_") {
		return false
	}
	if override != nil {
		return *override
	}
	_, ok := defaultCancellableDocTypes[docType]
	return ok
}

// EnsureReversalAllowed áp danh mục loại chứng từ được hủy cho một chứng từ
// gốc; trả ErrReversalNotAllowed (bọc) khi không được đảo trực tiếp.
func (s *PostingService) EnsureReversalAllowed(ctx context.Context, tenantID, domain, docType string) error {
	var override *bool
	if domain == reversalDomainFinance && strings.HasPrefix(docType, "FIN_") {
		policy, found, err := s.repo.LoadPostingPolicy(ctx, tenantID, docType)
		if err != nil {
			return err
		}
		if found && policy.AllowCancel.Valid {
			v := policy.AllowCancel.Bool
			override = &v
		}
	}
	if !reversalAllowed(domain, docType, override) {
		return fmt.Errorf("%w: chứng từ %s/%s không thuộc danh mục loại chứng từ được hủy; chứng từ nghiệp vụ phải hoàn tác qua service nghiệp vụ",
			ErrReversalNotAllowed, domain, docType)
	}
	return nil
}
