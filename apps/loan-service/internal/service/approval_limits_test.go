package service

import (
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// TestPickApprovalLimitPrecedence covers the lnm_approval_limits lookup
// precedence: exact product row > org-wide row > no config. The SQL side
// narrows to (product, org) rows; this test pins the Go-side pick so the
// fallback order cannot silently flip.
func TestPickApprovalLimitPrecedence(t *testing.T) {
	orgRow := &repository.ApprovalLimit{OrgCode: "LNM_PGD", ProductCode: "", PGDLimitMinor: 500_000_000, GDLimitMinor: 2_000_000_000}
	productRow := &repository.ApprovalLimit{OrgCode: "LNM_PGD", ProductCode: "TERM06", PGDLimitMinor: 1_000_000_000, GDLimitMinor: 4_000_000_000}

	// Product row wins over the org-wide row.
	picked := PickApprovalLimit(orgRow, productRow)
	if picked.ProductCode != "TERM06" || picked.PGDLimitMinor != 1_000_000_000 || picked.GDLimitMinor != 4_000_000_000 {
		t.Fatalf("product row must win: %+v", picked)
	}

	// No product row → org-wide fallback.
	picked = PickApprovalLimit(orgRow, nil)
	if picked.ProductCode != "" || picked.PGDLimitMinor != 500_000_000 || picked.GDLimitMinor != 2_000_000_000 {
		t.Fatalf("org-wide fallback expected: %+v", picked)
	}

	// No row at all → zero struct (ResolveApprovalLimits escalates it).
	picked = PickApprovalLimit(nil, nil)
	if picked.ProductCode != "" || picked.OrgCode != "" {
		t.Fatalf("no row must yield the zero struct: %+v", picked)
	}
}

// TestResolveApprovalLimitsMissingConfigEscalatesToBoard: AP-APR-01 — thiếu
// cấu hình hạn mức KHÔNG được hạ hồ sơ về cấp thực thi mặc định (fail-open,
// sentinel MaxInt64 cũ). Biến BPMN phải làm cả hai điều kiện
// `amount > gdLimit` và `amount > pgdLimit` đúng với mọi khoản vay dương, để
// GW_ApprovalLevel đẩy hồ sơ lên Hội đồng (cấp cao nhất).
func TestResolveApprovalLimitsMissingConfigEscalatesToBoard(t *testing.T) {
	pgd, gd, err := ResolveApprovalLimits(nil, nil)
	if err != nil {
		t.Fatalf("thiếu cấu hình phải đẩy lên cấp cao nhất, không lỗi: %v", err)
	}
	const smallestLoan int64 = 1
	if !(smallestLoan > gd) || !(smallestLoan > pgd) {
		t.Fatalf("thiếu cấu hình phải đưa mọi khoản vay dương lên Hội đồng: pgd=%d gd=%d", pgd, gd)
	}
}

// TestResolveApprovalLimitsInvalidConfigFailsClosed: cấu hình sai (hạn mức
// không dương, hoặc hạn mức GĐ nhỏ hơn hạn mức PGD) phải chặn nộp hồ sơ.
func TestResolveApprovalLimitsInvalidConfigFailsClosed(t *testing.T) {
	cases := map[string]*repository.ApprovalLimit{
		"pgd bằng 0":     {OrgCode: "LNM_PGD", PGDLimitMinor: 0, GDLimitMinor: 2_000_000_000},
		"pgd âm":         {OrgCode: "LNM_PGD", PGDLimitMinor: -1, GDLimitMinor: 2_000_000_000},
		"gd nhỏ hơn pgd": {OrgCode: "LNM_PGD", PGDLimitMinor: 2_000_000_000, GDLimitMinor: 500_000_000},
	}
	for name, row := range cases {
		_, _, err := ResolveApprovalLimits(row, nil)
		var appErr *ardaerrors.Error
		if !errors.As(err, &appErr) || appErr.Code != ardaerrors.CodeConflict {
			t.Fatalf("%s: cấu hình sai phải trả lỗi conflict (fail-closed), nhận %v", name, err)
		}
	}
}

// TestResolveApprovalLimitsValidConfig: cấu hình đúng được truyền nguyên vẹn.
func TestResolveApprovalLimitsValidConfig(t *testing.T) {
	org := &repository.ApprovalLimit{OrgCode: "LNM_PGD", PGDLimitMinor: 500_000_000, GDLimitMinor: 2_000_000_000}
	pgd, gd, err := ResolveApprovalLimits(org, nil)
	if err != nil || pgd != 500_000_000 || gd != 2_000_000_000 {
		t.Fatalf("cấu hình hợp lệ: pgd=%d gd=%d err=%v", pgd, gd, err)
	}
}
