// Package outbox relays rows from a service-owned transactional outbox table
// to NATS JetStream.
//
// A domain write inserts its event row in the same database transaction as the
// business change. The relay then claims pending rows with
// FOR UPDATE SKIP LOCKED, so concurrent replicas never publish the same row,
// publishes each row with the row id as Nats-Msg-Id, so a republish after a
// commit failure is deduplicated by JetStream, and marks the row published in
// the same transaction that holds the claim. Failed rows stay pending with
// attempt accounting and are retried on the next tick.
//
// The table must have the columns id, payload, created_at, published_at,
// publish_attempts and last_error, plus the subject column when one is
// configured.
package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// StreamName is the JetStream stream that carries every Arda domain event.
const StreamName = "ARDA_EVENTS"

// StreamSubjects is the subject filter used when StreamName has to be created.
// It lists business domains one by one instead of "arda.>" so that it never
// overlaps ai-service's AI_EVENTS stream (arda.ai.*): JetStream refuses to
// create a stream whose subjects overlap an existing one, so a catch-all here
// makes whichever service starts second fail. Keep it identical to
// arda-infra/platform/manifests/nats/arda-events-stream.json, which provisions
// the stream before applications start. check-event-subjects.mjs verifies that
// every registered subject is covered and that no entry reaches arda.ai.*.
var StreamSubjects = []string{
	"arda.capital.>",
	"arda.crm.>",
	"arda.deposit.>",
	"arda.dlq.>",
	"arda.docs.>",
	"arda.finance.>",
	"arda.hrm.>",
	"arda.iam.>",
	"arda.interbank.>",
	"arda.knowledge.>",
	"arda.loan.>",
	"arda.mdm.>",
	"arda.media.>",
	"arda.notification.>",
	"arda.platform.>",
	"arda.report.>",
	"arda.statistical.>",
	"arda.workflow.>",
}

const (
	defaultInterval  = 2 * time.Second
	defaultBatchSize = 50
)

var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Publisher is the subset of nats.JetStreamContext the relay needs.
type Publisher interface {
	PublishMsg(*nats.Msg, ...nats.PubOpt) (*nats.PubAck, error)
}

// Table describes one service outbox table.
type Table struct {
	// Name is the outbox table, for example "fin_outbox".
	Name string
	// SubjectColumn is the column holding each row's NATS subject. Leave it
	// empty when every row of the table goes to FixedSubject.
	SubjectColumn string
	// FixedSubject is used when SubjectColumn is empty.
	FixedSubject string
	// ClearLastErrorOnPublish sets last_error to NULL when a row is published.
	// Leave it false when last_error is declared NOT NULL.
	ClearLastErrorOnPublish bool
}

func (t Table) validate() error {
	if !identifierPattern.MatchString(t.Name) {
		return fmt.Errorf("outbox table name %q is not a plain identifier", t.Name)
	}
	if t.SubjectColumn != "" {
		if !identifierPattern.MatchString(t.SubjectColumn) {
			return fmt.Errorf("outbox subject column %q is not a plain identifier", t.SubjectColumn)
		}
		if t.FixedSubject != "" {
			return errors.New("outbox table sets both SubjectColumn and FixedSubject")
		}
		return nil
	}
	if !strings.HasPrefix(t.FixedSubject, "arda.") {
		return fmt.Errorf("outbox fixed subject %q must be an arda.* subject", t.FixedSubject)
	}
	return nil
}

// Option adjusts a Relay.
type Option func(*Relay)

// WithInterval sets the polling interval. Non-positive values are ignored.
func WithInterval(d time.Duration) Option {
	return func(r *Relay) {
		if d > 0 {
			r.interval = d
		}
	}
}

// WithBatchSize sets how many rows one tick claims. Non-positive values are
// ignored.
func WithBatchSize(n int) Option {
	return func(r *Relay) {
		if n > 0 {
			r.batchSize = n
		}
	}
}

// Relay publishes pending rows of one outbox table.
type Relay struct {
	db        *sql.DB
	publisher Publisher
	table     Table
	logger    *slog.Logger
	interval  time.Duration
	batchSize int

	claimSQL     string
	failureSQL   string
	publishedSQL string
}

// New builds a relay for table. It fails only when the table description is
// invalid.
func New(db *sql.DB, publisher Publisher, table Table, logger *slog.Logger, opts ...Option) (*Relay, error) {
	if db == nil {
		return nil, errors.New("outbox relay requires a database")
	}
	if publisher == nil {
		return nil, errors.New("outbox relay requires a publisher")
	}
	if err := table.validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	r := &Relay{
		db:        db,
		publisher: publisher,
		table:     table,
		logger:    logger.With("outbox", table.Name),
		interval:  defaultInterval,
		batchSize: defaultBatchSize,
	}
	for _, opt := range opts {
		opt(r)
	}

	subjectSelect := ""
	if table.SubjectColumn != "" {
		subjectSelect = ", " + table.SubjectColumn
	}
	r.claimSQL = fmt.Sprintf(
		`SELECT id::text, payload%s FROM %s WHERE published_at IS NULL ORDER BY created_at, id LIMIT $1 FOR UPDATE SKIP LOCKED`,
		subjectSelect, table.Name)
	r.failureSQL = fmt.Sprintf(
		`UPDATE %s SET publish_attempts = publish_attempts + 1, last_error = $2 WHERE id = $1`,
		table.Name)
	if table.ClearLastErrorOnPublish {
		r.publishedSQL = fmt.Sprintf(`UPDATE %s SET published_at = now(), last_error = NULL WHERE id = $1`, table.Name)
	} else {
		r.publishedSQL = fmt.Sprintf(`UPDATE %s SET published_at = now() WHERE id = $1`, table.Name)
	}
	return r, nil
}

// FromConn builds a relay on the shared ARDA_EVENTS stream. It returns nil,
// after logging the reason, when conn is nil, when JetStream itself is
// unavailable or when the table description is invalid, so a service keeps
// serving without an event bus; the rows stay pending until a relay runs.
// Run is safe on a nil Relay.
//
// A missing or uncreatable ARDA_EVENTS stream does not disable the relay:
// publishing then fails per row, the row stays pending with attempt
// accounting, and delivery resumes on its own once the stream exists.
func FromConn(db *sql.DB, conn *nats.Conn, table Table, logger *slog.Logger, opts ...Option) *Relay {
	if logger == nil {
		logger = slog.Default()
	}
	if conn == nil {
		return nil
	}
	js, err := conn.JetStream()
	if err != nil {
		logger.Error("outbox relay disabled", "outbox", table.Name, "err", err)
		return nil
	}
	if err := EnsureStream(js); err != nil {
		logger.Error("outbox relay: event stream unavailable; rows stay pending until it exists", "outbox", table.Name, "err", err)
	}
	relay, err := New(db, js, table, logger, opts...)
	if err != nil {
		logger.Error("outbox relay disabled", "outbox", table.Name, "err", err)
		return nil
	}
	return relay
}

// JetStream returns a JetStream context after EnsureStream succeeds.
func JetStream(conn *nats.Conn) (nats.JetStreamContext, error) {
	if conn == nil {
		return nil, errors.New("nats connection is nil")
	}
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream context: %w", err)
	}
	if err := EnsureStream(js); err != nil {
		return nil, err
	}
	return js, nil
}

// StreamManager is the subset of nats.JetStreamContext EnsureStream needs.
type StreamManager interface {
	StreamInfo(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error)
	AddStream(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error)
}

// EnsureStream keeps an existing ARDA_EVENTS stream exactly as provisioned
// and creates it with StreamSubjects only when it does not exist yet, for
// example on a local single-node NATS.
func EnsureStream(js StreamManager) error {
	_, err := js.StreamInfo(StreamName)
	if err == nil {
		return nil
	}
	if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("inspect stream %s: %w", StreamName, err)
	}
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:      StreamName,
		Subjects:  StreamSubjects,
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
	}); err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return fmt.Errorf("create stream %s: %w", StreamName, err)
	}
	return nil
}

// Run publishes on every tick until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) {
	if r == nil {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.logger.Info("outbox relay started", "interval", r.interval.String())
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("outbox relay stopped")
			return
		case <-ticker.C:
			if _, err := r.PublishOnce(ctx); err != nil && ctx.Err() == nil {
				r.logger.Error("outbox relay tick failed", "err", err)
			}
		}
	}
}

// Stats reports what one PublishOnce call did.
type Stats struct {
	Claimed   int
	Published int
	Failed    int
}

type pendingRow struct {
	id      string
	subject string
	payload []byte
}

// PublishOnce claims one batch, publishes it and commits. A per-row publish
// failure is recorded on the row and counted in Stats.Failed; the returned
// error covers only database failures, after which the claim is rolled back
// and every row stays pending.
func (r *Relay) PublishOnce(ctx context.Context) (Stats, error) {
	var stats Stats
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	batch, err := r.claim(ctx, tx)
	if err != nil {
		return stats, err
	}
	stats.Claimed = len(batch)

	for _, row := range batch {
		msg := &nats.Msg{Subject: row.subject, Data: row.payload, Header: nats.Header{}}
		msg.Header.Set(nats.MsgIdHdr, row.id)
		if _, err := r.publisher.PublishMsg(msg, nats.Context(ctx)); err != nil {
			stats.Failed++
			r.logger.Error("outbox relay: publish", "id", row.id, "subject", row.subject, "err", err)
			if _, updateErr := tx.ExecContext(ctx, r.failureSQL, row.id, err.Error()); updateErr != nil {
				return stats, fmt.Errorf("record publish failure for %s: %w", row.id, updateErr)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, r.publishedSQL, row.id); err != nil {
			return stats, fmt.Errorf("mark %s published: %w", row.id, err)
		}
		stats.Published++
	}
	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("commit: %w", err)
	}
	return stats, nil
}

func (r *Relay) claim(ctx context.Context, tx *sql.Tx) ([]pendingRow, error) {
	rows, err := tx.QueryContext(ctx, r.claimSQL, r.batchSize)
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	defer rows.Close()
	var batch []pendingRow
	for rows.Next() {
		var row pendingRow
		dest := []any{&row.id, &row.payload}
		if r.table.SubjectColumn != "" {
			dest = append(dest, &row.subject)
		} else {
			row.subject = r.table.FixedSubject
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	return batch, nil
}
