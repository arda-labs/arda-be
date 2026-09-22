package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// IndicatorRule is one threshold rule over a stored indicator value.
type IndicatorRule struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	IndicatorCode string    `json:"indicator_code"`
	DimensionKey  string    `json:"dimension_key,omitempty"`
	Operator      string    `json:"operator"`
	Threshold     float64   `json:"threshold"`
	Severity      string    `json:"severity"`
	Message       string    `json:"message,omitempty"`
	IsActive      bool      `json:"is_active"`
	CreatedBy     string    `json:"created_by,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// IndicatorAlert is one breach recorded for a rule + period + slice.
type IndicatorAlert struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	RuleCode       string     `json:"rule_code"`
	IndicatorCode  string     `json:"indicator_code"`
	DimensionKey   string     `json:"dimension_key,omitempty"`
	PeriodCode     string     `json:"period_code"`
	Value          *float64   `json:"value,omitempty"`
	Threshold      float64    `json:"threshold"`
	Operator       string     `json:"operator"`
	Severity       string     `json:"severity"`
	Message        string     `json:"message,omitempty"`
	Status         string     `json:"status"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const ruleColumns = `id::text, tenant_id, code, name, indicator_code, dimension_key,
	operator, threshold, severity, COALESCE(message,''), is_active,
	COALESCE(created_by,''), created_at, updated_at`

const alertColumns = `id::text, tenant_id, rule_code, indicator_code, dimension_key, period_code,
	value, threshold, operator, severity, COALESCE(message,''), status,
	COALESCE(acknowledged_by,''), acknowledged_at, created_at, updated_at`

// UpsertRule creates or updates one threshold rule.
func (r *StatisticalRepository) UpsertRule(ctx context.Context, rule *IndicatorRule) (*IndicatorRule, error) {
	if strings.TrimSpace(rule.Code) == "" || strings.TrimSpace(rule.IndicatorCode) == "" {
		return nil, errors.New("code and indicator_code are required")
	}
	if !ValidRuleOperator(rule.Operator) {
		return nil, fmt.Errorf("operator must be one of > >= < <= = <> (got %q)", rule.Operator)
	}
	if rule.Severity == "" {
		rule.Severity = "WARN"
	}
	if !ValidRuleSeverity(rule.Severity) {
		return nil, fmt.Errorf("severity must be INFO, WARN or CRITICAL (got %q)", rule.Severity)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_indicator_rules
		    (tenant_id, code, name, indicator_code, dimension_key, operator, threshold,
		     severity, message, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, code) DO UPDATE SET
		    name = EXCLUDED.name, indicator_code = EXCLUDED.indicator_code,
		    dimension_key = EXCLUDED.dimension_key, operator = EXCLUDED.operator,
		    threshold = EXCLUDED.threshold, severity = EXCLUDED.severity,
		    message = EXCLUDED.message, is_active = EXCLUDED.is_active,
		    updated_at = now(), version = rpt_indicator_rules.version + 1
		RETURNING `+ruleColumns,
		rule.TenantID, rule.Code, rule.Name, rule.IndicatorCode, rule.DimensionKey,
		rule.Operator, rule.Threshold, rule.Severity, rule.Message, rule.IsActive, rule.CreatedBy)
	return scanRule(row)
}

// ListRules returns the tenant's rules, optionally only the active ones.
func (r *StatisticalRepository) ListRules(ctx context.Context, tenantID string, onlyActive bool) ([]IndicatorRule, error) {
	where := "tenant_id = $1"
	if onlyActive {
		where += " AND is_active"
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM rpt_indicator_rules WHERE `+where+` ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IndicatorRule{}
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rule)
	}
	return out, rows.Err()
}

// UpsertAlert records a breach, replacing the earlier row for the same
// rule + period + slice so re-evaluating a period does not spam alerts.
func (r *StatisticalRepository) UpsertAlert(ctx context.Context, a *IndicatorAlert) (*IndicatorAlert, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_indicator_alerts
		    (tenant_id, rule_code, indicator_code, dimension_key, period_code,
		     value, threshold, operator, severity, message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (tenant_id, rule_code, period_code, dimension_key) DO UPDATE SET
		    value = EXCLUDED.value, threshold = EXCLUDED.threshold,
		    operator = EXCLUDED.operator, severity = EXCLUDED.severity,
		    message = EXCLUDED.message, updated_at = now()
		RETURNING `+alertColumns,
		a.TenantID, a.RuleCode, a.IndicatorCode, a.DimensionKey, a.PeriodCode,
		a.Value, a.Threshold, a.Operator, a.Severity, a.Message)
	return scanAlert(row)
}

// ClearAlert removes the alert for a rule + period + slice that is no longer
// breached, so a recovered indicator does not leave a stale open alert.
func (r *StatisticalRepository) ClearAlert(ctx context.Context, tenantID, ruleCode, periodCode, dimensionKey string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM rpt_indicator_alerts
		WHERE tenant_id = $1 AND rule_code = $2 AND period_code = $3 AND dimension_key = $4`,
		tenantID, ruleCode, periodCode, dimensionKey)
	return err
}

// ListAlerts returns the tenant's alerts, newest first.
func (r *StatisticalRepository) ListAlerts(ctx context.Context, tenantID, status, periodCode string) ([]IndicatorAlert, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	if periodCode != "" {
		args = append(args, periodCode)
		where = append(where, fmt.Sprintf("period_code = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+alertColumns+` FROM rpt_indicator_alerts WHERE `+joinAnd(where)+` ORDER BY created_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IndicatorAlert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// AckAlert marks one alert acknowledged.
func (r *StatisticalRepository) AckAlert(ctx context.Context, tenantID, id, actor string) error {
	tag, err := r.db.ExecContext(ctx, `
		UPDATE rpt_indicator_alerts
		SET status = 'ACKED', acknowledged_by = $3, acknowledged_at = now(), updated_at = now()
		WHERE tenant_id = $1 AND id = $2::uuid AND status = 'OPEN'`, tenantID, id, actor)
	if err != nil {
		return err
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return errors.New("alert not found or already acknowledged")
	}
	return nil
}

// IndicatorAlertSubject is the event subject notification-service consumes.
const IndicatorAlertSubject = "arda.statistical.indicator.breached.v1"

// EnqueueAlertEvent appends one alert event to the outbox for the relay to
// publish. dedupe_key makes it idempotent across COB re-runs: a breach that
// keeps the same value enqueues once, a materially different value enqueues
// again.
func (r *StatisticalRepository) EnqueueAlertEvent(ctx context.Context, a *IndicatorAlert) error {
	payload, err := json.Marshal(map[string]any{
		"rule_code":      a.RuleCode,
		"indicator_code": a.IndicatorCode,
		"period_code":    a.PeriodCode,
		"dimension_key":  a.DimensionKey,
		"value":          a.Value,
		"threshold":      a.Threshold,
		"operator":       a.Operator,
		"severity":       a.Severity,
		"message":        a.Message,
	})
	if err != nil {
		return err
	}
	dedupe := fmt.Sprintf("%s|%s|%s|%v", a.RuleCode, a.PeriodCode, a.DimensionKey, valueOrNil(a.Value))
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO rpt_outbox_events
		    (tenant_id, subject, aggregate_type, aggregate_id, dedupe_key, payload)
		VALUES ($1,$2,'indicator_alert',$3,$4,$5)
		ON CONFLICT (tenant_id, dedupe_key) DO NOTHING`,
		a.TenantID, IndicatorAlertSubject, a.RuleCode, dedupe, payload)
	return err
}

func valueOrNil(v *float64) any {
	if v == nil {
		return "nil"
	}
	return *v
}

// ValidRuleOperator is the closed set of comparison operators a rule may use.
func ValidRuleOperator(op string) bool {
	switch op {
	case ">", ">=", "<", "<=", "=", "<>":
		return true
	default:
		return false
	}
}

// ValidRuleSeverity is the closed severity set.
func ValidRuleSeverity(s string) bool {
	switch s {
	case "INFO", "WARN", "CRITICAL":
		return true
	default:
		return false
	}
}

func scanRule(row interface{ Scan(...any) error }) (*IndicatorRule, error) {
	var r IndicatorRule
	err := row.Scan(&r.ID, &r.TenantID, &r.Code, &r.Name, &r.IndicatorCode, &r.DimensionKey,
		&r.Operator, &r.Threshold, &r.Severity, &r.Message, &r.IsActive,
		&r.CreatedBy, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func scanAlert(row interface{ Scan(...any) error }) (*IndicatorAlert, error) {
	var a IndicatorAlert
	var value sql.NullFloat64
	var ackAt sql.NullTime
	err := row.Scan(&a.ID, &a.TenantID, &a.RuleCode, &a.IndicatorCode, &a.DimensionKey, &a.PeriodCode,
		&value, &a.Threshold, &a.Operator, &a.Severity, &a.Message, &a.Status,
		&a.AcknowledgedBy, &ackAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if value.Valid {
		a.Value = &value.Float64
	}
	if ackAt.Valid {
		a.AcknowledgedAt = &ackAt.Time
	}
	return &a, nil
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
