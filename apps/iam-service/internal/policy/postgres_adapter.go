package policy

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
)

// PostgresAdapter implements Casbin persist.Adapter backed by PostgreSQL.
type PostgresAdapter struct {
	db *sql.DB
}

// NewPostgresAdapter creates a Casbin adapter that reads policies from PostgreSQL.
func NewPostgresAdapter(db *sql.DB) *PostgresAdapter {
	return &PostgresAdapter{db: db}
}

func (a *PostgresAdapter) LoadPolicy(model model.Model) error {
	rows, err := a.db.Query(`
		SELECT ptype, v0, v1, v2, v3, v4, v5 FROM iam_casbin_rules ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("load casbin policies: %w", err)
	}
	defer rows.Close()

	var lineCount int
	for rows.Next() {
		var ptype, v0, v1, v2, v3, v4, v5 string
		if err := rows.Scan(&ptype, &v0, &v1, &v2, &v3, &v4, &v5); err != nil {
			return err
		}
		// Build the policy text: ptype, v0, v1, v2, ...
		var b strings.Builder
		b.WriteString(ptype)
		for _, v := range []string{v0, v1, v2, v3, v4, v5} {
			if v != "" {
				b.WriteString(", ")
				b.WriteString(v)
			}
		}
		persist.LoadPolicyLine(b.String(), model)
		lineCount++
	}

	slog.Info("casbin policies loaded from postgres", "count", lineCount)
	return rows.Err()
}

func (a *PostgresAdapter) SavePolicy(model model.Model) error {
	slog.Warn("SavePolicy not implemented — use iam_casbin_rules table directly")
	return nil
}

func (a *PostgresAdapter) AddPolicy(sec, ptype string, rule []string) error {
	return a.execInsert(ptype, rule)
}

func (a *PostgresAdapter) RemovePolicy(sec, ptype string, rule []string) error {
	return a.execDelete(ptype, rule)
}

func (a *PostgresAdapter) RemoveFilteredPolicy(sec, ptype string, fieldIndex int, fieldValues ...string) error {
	if len(fieldValues) == 0 {
		return nil
	}

	var clause strings.Builder
	clause.WriteString("1=1")
	args := []any{}

	for i, fv := range fieldValues {
		if fv == "" {
			continue
		}
		col := fmt.Sprintf("v%d", fieldIndex+i)
		clause.WriteString(fmt.Sprintf(" AND %s = $%d", col, len(args)+1))
		args = append(args, fv)
	}
	whereClause := clause.String()

	query := fmt.Sprintf("DELETE FROM iam_casbin_rules WHERE ptype = $1 AND %s", whereClause)
	finalArgs := append([]any{ptype}, args...)

	_, err := a.db.Exec(query, finalArgs...)
	return err
}

func (a *PostgresAdapter) execInsert(ptype string, rule []string) error {
	vals := padRule(rule)
	_, err := a.db.Exec(`
		INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT DO NOTHING
	`, ptype, vals[0], vals[1], vals[2], vals[3], vals[4], vals[5])
	return err
}

func (a *PostgresAdapter) execDelete(ptype string, rule []string) error {
	vals := padRule(rule)
	_, err := a.db.Exec(`
		DELETE FROM iam_casbin_rules
		WHERE ptype = $1 AND v0 = $2 AND v1 = $3 AND v2 = $4 AND v3 = $5 AND v4 = $6 AND v5 = $7
	`, ptype, vals[0], vals[1], vals[2], vals[3], vals[4], vals[5])
	return err
}

func padRule(rule []string) [6]string {
	var padded [6]string
	for i, s := range rule {
		if i < 6 {
			padded[i] = s
		}
	}
	return padded
}

// Ensure adapter implements persist.Adapter
var _ persist.Adapter = (*PostgresAdapter)(nil)
