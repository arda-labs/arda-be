package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

func main() {
	loanDSN := strings.TrimSpace(os.Getenv("DATABASE_DSN"))
	platformDSN := strings.TrimSpace(os.Getenv("PLATFORM_DATABASE_DSN"))
	if loanDSN == "" || platformDSN == "" {
		log.Fatal("DATABASE_DSN and PLATFORM_DATABASE_DSN are required")
	}
	loanDB, err := sql.Open("pgx/v5", loanDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer loanDB.Close()
	platformDB, err := sql.Open("pgx/v5", platformDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer platformDB.Close()
	if err := TransferGeneralProvisionRate(context.Background(), loanDB, platformDB); err != nil {
		log.Fatal(err)
	}
	log.Print("general provision rate is present and verified in platform-service")
}

// TransferGeneralProvisionRate copies the legacy all-history '%' row to the
// Platform global scope. Existing org rows and conflicting target values fail
// closed; repeating a successful transfer is safe.
func TransferGeneralProvisionRate(ctx context.Context, loanDB, platformDB *sql.DB) error {
	rows, err := loanDB.QueryContext(ctx, `SELECT org_code, rate_percent::text FROM lnm_general_provision_rates ORDER BY org_code`)
	if err != nil {
		return fmt.Errorf("read legacy provision rates: %w", err)
	}
	defer rows.Close()
	var legacy string
	count := 0
	for rows.Next() {
		var org, value string
		if err := rows.Scan(&org, &value); err != nil {
			return err
		}
		count++
		if org != "%" {
			return fmt.Errorf("cannot transfer org-specific legacy rate %q without an approved tenant/org mapping", org)
		}
		legacy = value
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("expected exactly one legacy '%%' rate, found %d", count)
	}
	legacyValue, err := decimal.NewFromString(legacy)
	if err != nil {
		return fmt.Errorf("parse legacy rate: %w", err)
	}

	tx, err := platformDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin platform transfer: %w", err)
	}
	defer tx.Rollback()
	var existingValue, existingType, existingUnit string
	var existingFrom string
	var existingTo sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT value, value_type, unit, effective_from::text, effective_to::text
		FROM plt_system_parameters WHERE module='loan' AND key='LNM_GENERAL_PROVISION_RATE'
		AND scope_type='global' AND tenant_id IS NULL AND scope_id IS NULL FOR UPDATE`).
		Scan(&existingValue, &existingType, &existingUnit, &existingFrom, &existingTo)
	if err == nil {
		parsed, parseErr := decimal.NewFromString(existingValue)
		if parseErr != nil || !parsed.Equal(legacyValue) || !strings.EqualFold(existingType, "decimal") || existingUnit != "percent" || existingFrom != "0001-01-01" || existingTo.Valid {
			return errors.New("Platform already has a conflicting general provision rate; manual reconciliation is required")
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check existing Platform rate: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plt_system_parameters
		(id, module, key, value, value_type, unit, scope_type, effective_from)
		VALUES ('param_lnm_general_provision_rate', 'loan', 'LNM_GENERAL_PROVISION_RATE', $1, 'decimal', 'percent', 'global', DATE '0001-01-01')`, legacy)
	if err != nil {
		return fmt.Errorf("insert Platform rate: %w", err)
	}
	var verifyValue, verifyType, verifyUnit string
	err = tx.QueryRowContext(ctx, `SELECT value, value_type, unit FROM plt_system_parameters
		WHERE module='loan' AND key='LNM_GENERAL_PROVISION_RATE' AND scope_type='global'
		AND tenant_id IS NULL AND scope_id IS NULL AND effective_from=DATE '0001-01-01' AND effective_to IS NULL`).
		Scan(&verifyValue, &verifyType, &verifyUnit)
	if err != nil {
		return fmt.Errorf("verify Platform rate: %w", err)
	}
	verified, parseErr := decimal.NewFromString(verifyValue)
	if parseErr != nil || !verified.Equal(legacyValue) || !strings.EqualFold(verifyType, "decimal") || verifyUnit != "percent" {
		return errors.New("Platform rate readback did not match the legacy value")
	}
	return tx.Commit()
}
