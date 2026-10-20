package service

import (
	"fmt"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// approvalLimitEscalateToBoard là giá trị pgdLimit/gdLimit khi tenant+org
// chưa cấu hình hạn mức phê duyệt (AP-APR-01). Với mọi khoản vay dương
// (CreateContract đã chặn loan_amt <= 0), cả `amount > gdLimit` lẫn
// `amount > pgdLimit` đều đúng, nên GW_ApprovalLevel của
// lnm-loan-formation-v2 đi nhánh Flow_Level_Board (khai báo trước
// Flow_Level_GD — scripts/check-approval-limits.mjs giữ thứ tự này): thiếu
// cấu hình thì đẩy lên Hội đồng, không bao giờ hạ về cấp thực thi mặc định
// như sentinel MaxInt64 cũ (fail-open).
const approvalLimitEscalateToBoard int64 = 0

// PickApprovalLimit resolves which lnm_approval_limits row feeds the
// formation approval-tier variables: the exact product row when present,
// otherwise the org-wide fallback row (product_code = ”). Passing
// product=nil (no product row found) selects the org-wide row; org=nil or
// both nil returns the empty struct, which ResolveApprovalLimits escalates
// to the Board tier. Pure function — unit-tested without a DB.
func PickApprovalLimit(org, product *repository.ApprovalLimit) repository.ApprovalLimit {
	if product != nil {
		return *product
	}
	if org != nil {
		return *org
	}
	return repository.ApprovalLimit{}
}

// ResolveApprovalLimits trả về cặp biến BPMN (pgdLimit, gdLimit) cho một hồ
// sơ, theo nguyên tắc fail-closed của AP-APR-01:
//   - không có dòng cấu hình nào → đẩy lên cấp cao nhất (Hội đồng);
//   - dòng cấu hình sai (hạn mức không dương, hoặc hạn mức GĐ < hạn mức PGD)
//     → lỗi conflict, chặn nộp hồ sơ;
//   - dòng hợp lệ → dùng nguyên giá trị.
//
// Hàm thuần, test không cần DB.
func ResolveApprovalLimits(org, product *repository.ApprovalLimit) (pgd, gd int64, err error) {
	picked := PickApprovalLimit(org, product)
	if picked.OrgCode == "" {
		return approvalLimitEscalateToBoard, approvalLimitEscalateToBoard, nil
	}
	if picked.PGDLimitMinor <= 0 || picked.GDLimitMinor < picked.PGDLimitMinor {
		return 0, 0, ardaerrors.New(ardaerrors.CodeConflict, fmt.Sprintf(
			"approval_limit_invalid: org %q product %q has pgd_limit_minor=%d gd_limit_minor=%d (need 0 < pgd <= gd)",
			picked.OrgCode, picked.ProductCode, picked.PGDLimitMinor, picked.GDLimitMinor))
	}
	return picked.PGDLimitMinor, picked.GDLimitMinor, nil
}
