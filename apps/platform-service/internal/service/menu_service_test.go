package service

import (
	"context"
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

func TestMenuUpsertRejectsMalformedSection(t *testing.T) {
	svc := NewMenuService(nil)
	for _, section := range []string{"Credit", "phòng tín dụng", "credit-dept", "1credit"} {
		_, err := svc.Upsert(context.Background(), "tenant-1", domain.MenuItem{
			Code:    "admin.loan",
			Title:   "Tín dụng",
			Section: section,
		})
		var appErr *ardaerrors.Error
		if !errors.As(err, &appErr) || appErr.Code != ardaerrors.CodeInvalidInput {
			t.Fatalf("section %q: want %s, got %v", section, ardaerrors.CodeInvalidInput, err)
		}
	}
}

func TestMenuSectionPatternAcceptsCodes(t *testing.T) {
	for _, section := range []string{"credit", "funding", "accounting", "control", "hr", "shared", "admin", "branch_ops2"} {
		if !menuSectionPattern.MatchString(section) {
			t.Fatalf("section %q should be accepted", section)
		}
	}
}
