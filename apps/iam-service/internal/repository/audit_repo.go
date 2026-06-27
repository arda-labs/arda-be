package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

// AuditRepository provides query and persistence for audit logs.
type AuditRepository struct {
	db *sql.DB
}

// NewAuditRepository creates an audit repository.
func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// InsertWithChain inserts an audit log with hash chain verification.
func (r *AuditRepository) InsertWithChain(ctx context.Context, e *domain.AuthEvent) error {
	prevHash := r.getLastHash(ctx)

	if e.ID == "" {
		e.ID = fmt.Sprintf("%s-%d", e.EventType, time.Now().UnixNano())
	}

	src := prevHash + e.ID + e.Timestamp.String() + e.Result
	chainHash := sha256Hex(src)
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details["chain_prev"] = prevHash
	e.Details["chain_hash"] = chainHash

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_audit_logs
			(event_id, event_type, subject, action, resource, result,
			 details, client_ip, user_agent, request_id, service_name,
			 tenant_id, chain_prev_hash, chain_hash, timestamp)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (event_id) DO NOTHING
	`, e.ID, e.EventType, e.Subject, e.Action, e.Resource, e.Result,
		marshalDetails(e.Details), e.ClientIP, e.UserAgent, e.RequestID,
		e.ServiceName, e.Details["tenant_id"], prevHash, chainHash, e.Timestamp)
	return err
}

type QueryParams struct {
	EventTypes []string
	Subject    string
	Result     string
	TenantID   string
	From       time.Time
	To         time.Time
	Page       int
	Size       int
	Sort       string
}

func (r *AuditRepository) Query(ctx context.Context, params QueryParams) ([]domain.AuthEvent, int, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1

	if len(params.EventTypes) > 0 {
		placeholders := make([]string, len(params.EventTypes))
		for i, et := range params.EventTypes {
			placeholders[i] = fmt.Sprintf("$%d", idx)
			args = append(args, et)
			idx++
		}
		where = append(where, fmt.Sprintf("event_type IN (%s)", strings.Join(placeholders, ",")))
	}
	if params.Subject != "" {
		where = append(where, fmt.Sprintf("subject ILIKE $%d", idx))
		args = append(args, "%"+params.Subject+"%")
		idx++
	}
	if params.Result != "" {
		where = append(where, fmt.Sprintf("result = $%d", idx))
		args = append(args, params.Result)
		idx++
	}
	if params.TenantID != "" {
		where = append(where, fmt.Sprintf("tenant_id = $%d", idx))
		args = append(args, params.TenantID)
		idx++
	}
	if !params.From.IsZero() {
		where = append(where, fmt.Sprintf("timestamp >= $%d", idx))
		args = append(args, params.From)
		idx++
	}
	if !params.To.IsZero() {
		where = append(where, fmt.Sprintf("timestamp <= $%d", idx))
		args = append(args, params.To)
		idx++
	}

	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM iam_audit_logs WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if params.Page < 1 {
		params.Page = 1
	}
	if params.Size < 1 || params.Size > 500 {
		params.Size = 10
	}
	offset := (params.Page - 1) * params.Size

	order := "timestamp DESC"
	if params.Sort == "timestamp" {
		order = "timestamp ASC"
	}

	query := fmt.Sprintf(`
		SELECT event_id, event_type, subject, action, resource, result,
		       details, client_ip, user_agent, request_id, service_name,
		       tenant_id, chain_prev_hash, chain_hash, timestamp
		FROM iam_audit_logs
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, wc, order, idx, idx+1)
	allArgs := append(args, params.Size, offset)

	rows, err := r.db.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []domain.AuthEvent
	for rows.Next() {
		var e domain.AuthEvent
		var detailsJSON sql.NullString
		var tenantID sql.NullString
		var chainPrev, chainHash sql.NullString
		var requestID sql.NullString

		err := rows.Scan(&e.ID, &e.EventType, &e.Subject, &e.Action, &e.Resource, &e.Result,
			&detailsJSON, &e.ClientIP, &e.UserAgent, &requestID, &e.ServiceName,
			&tenantID, &chainPrev, &chainHash, &e.Timestamp)
		if err != nil {
			return nil, 0, err
		}
		e.RequestID = requestID.String
		e.Details = make(map[string]any)
		if detailsJSON.Valid {
			json.Unmarshal([]byte(detailsJSON.String), &e.Details)
		}
		if tenantID.Valid {
			e.Details["tenant_id"] = tenantID.String
		}
		if chainPrev.Valid {
			e.Details["chain_prev"] = chainPrev.String
		}
		if chainHash.Valid {
			e.Details["chain_hash"] = chainHash.String
		}
		events = append(events, e)
	}
	return events, total, rows.Err()
}

type AuditStats struct {
	TotalEvents  int            `json:"totalEvents"`
	ByEventType  map[string]int `json:"byEventType"`
	ByResult     map[string]int `json:"byResult"`
	LoginSuccess int            `json:"loginSuccess"`
	LoginFailure int            `json:"loginFailure"`
	From         time.Time      `json:"from"`
	To           time.Time      `json:"to"`
}

func (r *AuditRepository) Stats(ctx context.Context, from, to time.Time) (*AuditStats, error) {
	stats := &AuditStats{
		From:        from,
		To:          to,
		ByEventType: make(map[string]int),
		ByResult:    make(map[string]int),
	}

	r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM iam_audit_logs WHERE timestamp >= $1 AND timestamp <= $2
	`, from, to).Scan(&stats.TotalEvents)

	rows, err := r.db.QueryContext(ctx, `
		SELECT event_type, COUNT(*) FROM iam_audit_logs
		WHERE timestamp >= $1 AND timestamp <= $2
		GROUP BY event_type ORDER BY COUNT(*) DESC
	`, from, to)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var et string
			var count int
			if rows.Scan(&et, &count) == nil {
				stats.ByEventType[et] = count
			}
		}
	}

	rows2, err := r.db.QueryContext(ctx, `
		SELECT result, COUNT(*) FROM iam_audit_logs
		WHERE timestamp >= $1 AND timestamp <= $2
		GROUP BY result
	`, from, to)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var res string
			var count int
			if rows2.Scan(&res, &count) == nil {
				stats.ByResult[res] = count
				if res == "success" {
					stats.LoginSuccess += count
				} else {
					stats.LoginFailure += count
				}
			}
		}
	}

	return stats, nil
}

type ChainVerification struct {
	Valid    bool     `json:"valid"`
	Total    int      `json:"total"`
	Tampered []string `json:"tampered,omitempty"`
}

func (r *AuditRepository) VerifyChain(ctx context.Context, from, to time.Time) (*ChainVerification, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT event_id, chain_prev_hash, chain_hash, timestamp, result
		FROM iam_audit_logs
		WHERE timestamp >= $1 AND timestamp <= $2
		ORDER BY timestamp ASC, created_at ASC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &ChainVerification{Valid: true}
	var prevHash string

	for rows.Next() {
		var eventID, chainPrev, chainHash string
		var ts time.Time
		var res string

		if err := rows.Scan(&eventID, &chainPrev, &chainHash, &ts, &res); err != nil {
			return nil, err
		}
		result.Total++

		expectedPrev := prevHash
		expectedHash := sha256Hex(expectedPrev + eventID + ts.String() + res)

		if chainPrev != expectedPrev || chainHash != expectedHash {
			result.Valid = false
			result.Tampered = append(result.Tampered, eventID)
		}

		prevHash = chainHash
	}

	return result, nil
}

// ── Retention ──

func (r *AuditRepository) PurgeOlderThan(ctx context.Context, before time.Time) (int, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM iam_audit_logs WHERE timestamp < $1`, before)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (r *AuditRepository) PurgeByEventType(ctx context.Context, eventType string, before time.Time) (int, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM iam_audit_logs WHERE event_type = $1 AND timestamp < $2`, eventType, before)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (r *AuditRepository) getLastHash(ctx context.Context) string {
	var hash string
	err := r.db.QueryRowContext(ctx, `
		SELECT chain_hash FROM iam_audit_logs
		ORDER BY timestamp DESC, created_at DESC LIMIT 1
	`).Scan(&hash)
	if err != nil {
		return ""
	}
	return hash
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
