package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

type seededPostingRules map[string][]*financev1.PostingRule

func (s seededPostingRules) ListPostingRules(_ context.Context, documentType string) ([]*financev1.PostingRule, error) {
	return s[documentType], nil
}

func TestSeededPostingCardsResolveAndBalance(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	rows, err := db.Query(`
		SELECT DISTINCT ON (document_type, line_no)
		       document_type, line_no, direction, resolution_type,
		       COALESCE(account_ref,''), COALESCE(acc_classification,'')
		FROM fin_accounting_rules
		WHERE is_active AND (document_type LIKE 'LNM_%' OR document_type LIKE 'DPM_%'
		  OR document_type LIKE 'IBM_%' OR document_type LIKE 'FUND_%'
		  OR document_type LIKE 'CFC_%' OR document_type LIKE 'VCM_%')
		ORDER BY document_type, line_no, tenant_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cards := map[string][]*financev1.PostingRule{}
	for rows.Next() {
		var docType, direction, resolution, account, classification string
		var line int32
		if err := rows.Scan(&docType, &line, &direction, &resolution, &account, &classification); err != nil {
			t.Fatal(err)
		}
		cards[docType] = append(cards[docType], &financev1.PostingRule{LineNo: line, Direction: direction, ResolutionType: resolution, AccountRef: account, AccClassification: classification})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, docType := range []string{"LNM_DISBURSEMENT", "LNM_COLLECTION", "LNM_ACCRUAL", "LNM_PROVISION", "LNM_PROVISION_306", "DPM_SETTLEMENT_V3", "LNM_DISB_REGISTER", "LNM_DISB_COMPLETE"} {
		if len(cards[docType]) == 0 {
			t.Errorf("required card %s is not seeded", docType)
		}
	}
	for _, prefix := range []string{"DPM_", "IBM_", "FUND_", "CFC_", "VCM_"} {
		found := false
		for docType := range cards {
			if strings.HasPrefix(docType, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no seeded %s posting cards", prefix)
		}
	}
	if len(cards) == 0 {
		t.Fatal("no active posting cards seeded")
	}
	for docType, rules := range cards {
		t.Run(docType, func(t *testing.T) {
			debitCount, creditCount := 0, 0
			for _, rule := range rules {
				switch rule.GetDirection() {
				case "DEBIT":
					debitCount++
				case "CREDIT":
					creditCount++
				default:
					t.Fatalf("line %d has invalid direction %q", rule.GetLineNo(), rule.GetDirection())
				}
			}
			if debitCount == 0 || creditCount == 0 {
				t.Fatalf("card must have both sides: debit=%d credit=%d", debitCount, creditCount)
			}
			debitTotal := int64(debitCount * 100)
			creditRemainder := debitTotal - int64(creditCount-1)
			if creditRemainder <= 0 {
				t.Fatalf("cannot build balanced fixture for %s", docType)
			}
			legs := make([]financeclient.PostingLeg, 0, len(rules))
			creditIndex := 0
			for _, rule := range rules {
				amount := int64(100)
				if rule.GetDirection() == "CREDIT" {
					creditIndex++
					if creditIndex == creditCount {
						amount = creditRemainder
					} else {
						amount = 1
					}
				}
				legs = append(legs, financeclient.PostingLeg{CardLine: rule.GetLineNo(), Direction: rule.GetDirection(), AmountMinor: amount, Analytics: &financev1.Analytics{OrgUnitCode: "TEST"}})
			}
			lines, err := financeclient.BuildPostingLines(context.Background(), seededPostingRules(cards), docType, legs, "VND")
			if err != nil {
				t.Fatal(err)
			}
			var debit, credit int64
			for _, line := range lines {
				switch line.GetDirection() {
				case "DEBIT":
					debit += line.GetAmountMinor()
				case "CREDIT":
					credit += line.GetAmountMinor()
				}
			}
			if debit != credit {
				t.Fatalf("built lines are unbalanced: debit=%d credit=%d", debit, credit)
			}
		})
	}
}
