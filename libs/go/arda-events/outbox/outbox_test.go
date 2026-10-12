package outbox

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	"github.com/nats-io/nats.go"
)

func TestTableValidation(t *testing.T) {
	cases := []struct {
		name    string
		table   Table
		wantErr string
	}{
		{name: "fixed subject", table: Table{Name: "fin_outbox", FixedSubject: "arda.finance.transaction.posted"}},
		{name: "subject column", table: Table{Name: "loan_outbox", SubjectColumn: "subject"}},
		{name: "injected table", table: Table{Name: "x; DROP TABLE y", SubjectColumn: "subject"}, wantErr: "plain identifier"},
		{name: "injected column", table: Table{Name: "t", SubjectColumn: "subject, payload"}, wantErr: "plain identifier"},
		{name: "both subjects", table: Table{Name: "t", SubjectColumn: "subject", FixedSubject: "arda.x.y"}, wantErr: "both"},
		{name: "no subject", table: Table{Name: "t"}, wantErr: "arda.*"},
		{name: "foreign subject", table: Table{Name: "t", FixedSubject: "other.x"}, wantErr: "arda.*"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.table.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	table := Table{Name: "t", SubjectColumn: "subject"}
	if _, err := New(nil, &fakePublisher{}, table, nil); err == nil {
		t.Fatal("New without db succeeded")
	}
	if _, err := New(&sql.DB{}, nil, table, nil); err == nil {
		t.Fatal("New without publisher succeeded")
	}
}

func TestFromConnWithoutConnectionIsNilAndRunIsSafe(t *testing.T) {
	relay := FromConn(&sql.DB{}, nil, Table{Name: "t", SubjectColumn: "subject"}, slog.New(slog.DiscardHandler))
	if relay != nil {
		t.Fatal("FromConn(nil conn) returned a relay")
	}
	relay.Run(context.Background())
}

// fakePublisher records published messages. It can block the first publish
// until released, and fail the first publish.
type fakePublisher struct {
	blockFirst   bool
	firstEntered chan struct{}
	releaseFirst chan struct{}
	failFirst    bool

	mu         sync.Mutex
	calls      int
	messageIDs []string
	subjects   []string
	accepted   map[string]struct{}
}

func (p *fakePublisher) PublishMsg(msg *nats.Msg, _ ...nats.PubOpt) (*nats.PubAck, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	id := msg.Header.Get(nats.MsgIdHdr)
	p.messageIDs = append(p.messageIDs, id)
	p.subjects = append(p.subjects, msg.Subject)
	p.mu.Unlock()
	if call == 1 && p.blockFirst {
		close(p.firstEntered)
		<-p.releaseFirst
	}
	if call == 1 && p.failFirst {
		return nil, errors.New("jetstream unavailable")
	}
	p.mu.Lock()
	if p.accepted == nil {
		p.accepted = map[string]struct{}{}
	}
	p.accepted[id] = struct{}{}
	p.mu.Unlock()
	return &nats.PubAck{Stream: StreamName}, nil
}

const testSchema = `
CREATE TABLE fixed_outbox (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    payload          JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at     TIMESTAMPTZ,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT
);
CREATE TABLE subject_outbox (
    id               VARCHAR(64) PRIMARY KEY,
    subject          VARCHAR(160) NOT NULL,
    payload          JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at     TIMESTAMPTZ,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT NOT NULL DEFAULT ''
);`

var (
	fixedTable   = Table{Name: "fixed_outbox", FixedSubject: "arda.test.fixed.v1", ClearLastErrorOnPublish: true}
	subjectTable = Table{Name: "subject_outbox", SubjectColumn: "subject"}
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.Open(t, func(db *sql.DB) error {
		_, err := db.Exec(testSchema)
		return err
	})
}

func newRelay(t *testing.T, db *sql.DB, publisher Publisher, table Table) *Relay {
	t.Helper()
	relay, err := New(db, publisher, table, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return relay
}

func insertFixed(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO fixed_outbox (payload) VALUES ('{"n":1}') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestConcurrentRelaysPublishEachRowOnce(t *testing.T) {
	db := openDB(t)
	id := insertFixed(t, db)
	publisher := &fakePublisher{blockFirst: true, firstEntered: make(chan struct{}), releaseFirst: make(chan struct{})}
	first := newRelay(t, db, publisher, fixedTable)
	second := newRelay(t, db, publisher, fixedTable)

	done := make(chan Stats)
	go func() {
		stats, err := first.PublishOnce(context.Background())
		if err != nil {
			t.Error(err)
		}
		done <- stats
	}()
	select {
	case <-publisher.firstEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first relay did not reach publish")
	}
	secondStats, err := second.PublishOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if secondStats.Claimed != 0 {
		t.Fatalf("second relay claimed %d rows locked by the first", secondStats.Claimed)
	}
	close(publisher.releaseFirst)
	firstStats := <-done
	if firstStats != (Stats{Claimed: 1, Published: 1}) {
		t.Fatalf("first relay stats = %+v", firstStats)
	}

	var publishedAt sql.NullTime
	var lastError sql.NullString
	if err := db.QueryRow(`SELECT published_at, last_error FROM fixed_outbox WHERE id = $1`, id).Scan(&publishedAt, &lastError); err != nil {
		t.Fatal(err)
	}
	if !publishedAt.Valid || lastError.Valid {
		t.Fatalf("row after publish: published=%t last_error=%v", publishedAt.Valid, lastError)
	}
	if len(publisher.messageIDs) != 1 || publisher.messageIDs[0] != id || publisher.subjects[0] != fixedTable.FixedSubject {
		t.Fatalf("published ids=%v subjects=%v, want [%s] [%s]", publisher.messageIDs, publisher.subjects, id, fixedTable.FixedSubject)
	}
}

func TestFailedPublishRetriesWithStableMessageID(t *testing.T) {
	db := openDB(t)
	id := insertFixed(t, db)
	publisher := &fakePublisher{failFirst: true}
	relay := newRelay(t, db, publisher, fixedTable)

	stats, err := relay.PublishOnce(context.Background())
	if err != nil || stats != (Stats{Claimed: 1, Failed: 1}) {
		t.Fatalf("first tick stats=%+v err=%v", stats, err)
	}
	var publishedAt sql.NullTime
	var attempts int
	var lastError sql.NullString
	if err := db.QueryRow(`SELECT published_at, publish_attempts, last_error FROM fixed_outbox WHERE id = $1`, id).Scan(&publishedAt, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if publishedAt.Valid || attempts != 1 || lastError.String != "jetstream unavailable" {
		t.Fatalf("after failure: published=%t attempts=%d last_error=%q", publishedAt.Valid, attempts, lastError.String)
	}

	stats, err = relay.PublishOnce(context.Background())
	if err != nil || stats != (Stats{Claimed: 1, Published: 1}) {
		t.Fatalf("retry stats=%+v err=%v", stats, err)
	}
	if err := db.QueryRow(`SELECT published_at, publish_attempts, last_error FROM fixed_outbox WHERE id = $1`, id).Scan(&publishedAt, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if !publishedAt.Valid || attempts != 1 || lastError.Valid {
		t.Fatalf("after retry: published=%t attempts=%d last_error=%v", publishedAt.Valid, attempts, lastError)
	}
	if len(publisher.messageIDs) != 2 || publisher.messageIDs[0] != id || publisher.messageIDs[1] != id {
		t.Fatalf("message ids = %v, want the row id twice", publisher.messageIDs)
	}
	if len(publisher.accepted) != 1 {
		t.Fatalf("accepted unique messages = %d, want 1", len(publisher.accepted))
	}

	stats, err = relay.PublishOnce(context.Background())
	if err != nil || stats != (Stats{}) {
		t.Fatalf("idle tick stats=%+v err=%v", stats, err)
	}
}

func TestSubjectColumnAndNotNullLastError(t *testing.T) {
	db := openDB(t)
	if _, err := db.Exec(`
		INSERT INTO subject_outbox (id, subject, payload, created_at) VALUES
		  ('evt-b', 'arda.test.second.v1', '{"n":2}', now()),
		  ('evt-a', 'arda.test.first.v1',  '{"n":1}', now() - interval '1 minute')`); err != nil {
		t.Fatal(err)
	}
	publisher := &fakePublisher{failFirst: true}
	relay := newRelay(t, db, publisher, subjectTable)

	stats, err := relay.PublishOnce(context.Background())
	if err != nil || stats != (Stats{Claimed: 2, Published: 1, Failed: 1}) {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if got := strings.Join(publisher.subjects, ","); got != "arda.test.first.v1,arda.test.second.v1" {
		t.Fatalf("subjects in created_at order = %s", got)
	}
	stats, err = relay.PublishOnce(context.Background())
	if err != nil || stats != (Stats{Claimed: 1, Published: 1}) {
		t.Fatalf("retry stats=%+v err=%v", stats, err)
	}
	var lastError string
	var pending int
	if err := db.QueryRow(`SELECT last_error FROM subject_outbox WHERE id = 'evt-a'`).Scan(&lastError); err != nil {
		t.Fatal(err)
	}
	if lastError != "jetstream unavailable" {
		t.Fatalf("last_error kept for NOT NULL column = %q", lastError)
	}
	if err := db.QueryRow(`SELECT count(*) FROM subject_outbox WHERE published_at IS NULL`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending rows = %d, want 0", pending)
	}
}

func TestBatchSizeLimitsClaim(t *testing.T) {
	db := openDB(t)
	for range 3 {
		insertFixed(t, db)
	}
	relay, err := New(db, &fakePublisher{}, fixedTable, slog.New(slog.DiscardHandler), WithBatchSize(2), WithInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if relay.interval != time.Hour {
		t.Fatalf("interval = %s", relay.interval)
	}
	first, err := relay.PublishOnce(context.Background())
	if err != nil || first.Claimed != 2 {
		t.Fatalf("first batch = %+v err=%v", first, err)
	}
	second, err := relay.PublishOnce(context.Background())
	if err != nil || second.Claimed != 1 {
		t.Fatalf("second batch = %+v err=%v", second, err)
	}
}
