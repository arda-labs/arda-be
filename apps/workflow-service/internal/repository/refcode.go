package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func caseCodePrefix(caseType string) string {
	switch caseType {
	case "CUSTOMER_REGISTRATION":
		return "DKKH"
	case "HRM_EMPLOYEE_REGISTRATION":
		return "DKNV"
	default:
		parts := strings.Split(caseType, "_")
		if len(parts) > 0 && parts[0] != "" {
			return strings.ToUpper(parts[0])
		}
		return "CASE"
	}
}

func (r *CaseRepository) nextCaseCode(ctx context.Context, caseType string) (string, error) {
	var seq int64
	if err := r.db.QueryRowContext(ctx, `SELECT nextval('workflow_case_code_seq')`).Scan(&seq); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%06d", caseCodePrefix(caseType), time.Now().UTC().Format("20060102"), seq), nil
}
