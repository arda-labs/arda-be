package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// captureConnector builds a *sql.DB whose statements are recorded instead of
// reaching a database. It pins the push subscription ownership rules without
// requiring Postgres in CI.
type captureConnector struct {
	queries  []string
	args     [][]any
	row      []driver.Value
	noRows   bool
	affected int64
	queryErr error
	execErr  error
}

func (c *captureConnector) Connect(context.Context) (driver.Conn, error) {
	return &captureConn{c: c}, nil
}

func (c *captureConnector) Driver() driver.Driver { return captureDriver{} }

type captureDriver struct{}

func (captureDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("capture driver requires a connector")
}

type captureConn struct{ c *captureConnector }

func (c *captureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("capture driver only supports context methods")
}

func (c *captureConn) Close() error { return nil }

func (c *captureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("capture driver does not support transactions")
}

func (c *captureConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.c.queries = append(c.c.queries, query)
	c.c.args = append(c.c.args, namedValues(args))
	if c.c.queryErr != nil {
		return nil, c.c.queryErr
	}
	if c.c.noRows {
		return &captureRows{}, nil
	}
	return &captureRows{values: c.c.row}, nil
}

func (c *captureConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.c.queries = append(c.c.queries, query)
	c.c.args = append(c.c.args, namedValues(args))
	if c.c.execErr != nil {
		return nil, c.c.execErr
	}
	return driver.RowsAffected(c.c.affected), nil
}

type captureRows struct {
	values []driver.Value
	done   bool
}

func (r *captureRows) Columns() []string { return []string{"id"} }

func (r *captureRows) Close() error { return nil }

func (r *captureRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values)
	return nil
}

func namedValues(values []driver.NamedValue) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value.Value)
	}
	return out
}

func newCaptureDB(t *testing.T, connector *captureConnector) (*NotificationRepository, *captureConnector) {
	t.Helper()
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })
	return NewNotificationRepository(db), connector
}

func TestUpsertPushSubscriptionRejectsEndpointOwnedByAnotherUser(t *testing.T) {
	repo, captured := newCaptureDB(t, &captureConnector{noRows: true})

	err := repo.UpsertPushSubscription(context.Background(), PushSubscription{
		TenantID: "tenant-b",
		UserID:   "user-b",
		Endpoint: "https://push.example.com/abc",
		P256dh:   "key-b",
		Auth:     "auth-b",
	})
	if !errors.Is(err, ErrPushSubscriptionOwnedByAnotherUser) {
		t.Fatalf("UpsertPushSubscription = %v, want %v", err, ErrPushSubscriptionOwnedByAnotherUser)
	}
	if len(captured.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(captured.queries))
	}
	for _, guard := range []string{
		"noti_push_subscriptions.tenant_id = EXCLUDED.tenant_id",
		"noti_push_subscriptions.user_id = EXCLUDED.user_id",
	} {
		if !strings.Contains(captured.queries[0], guard) {
			t.Fatalf("upsert must keep the existing owner; missing guard %q in:\n%s", guard, captured.queries[0])
		}
	}
}

func TestUpsertPushSubscriptionUpdatesKeysForTheSameOwner(t *testing.T) {
	repo, captured := newCaptureDB(t, &captureConnector{row: []driver.Value{"sub-1"}})

	err := repo.UpsertPushSubscription(context.Background(), PushSubscription{
		TenantID: "tenant-a",
		UserID:   "user-a",
		Endpoint: "https://push.example.com/abc",
		P256dh:   "key-a2",
		Auth:     "auth-a2",
	})
	if err != nil {
		t.Fatalf("UpsertPushSubscription = %v, want nil", err)
	}
	wantArgs := []any{"tenant-a", "user-a", "https://push.example.com/abc", "key-a2", "auth-a2", ""}
	if got := captured.args[0]; !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("upsert args = %#v, want %#v", got, wantArgs)
	}
}

func TestDeletePushSubscriptionByEndpointIsUserScoped(t *testing.T) {
	repo, captured := newCaptureDB(t, &captureConnector{affected: 1})

	if err := repo.DeletePushSubscriptionByEndpoint(context.Background(),
		"tenant-a", "user-a", "https://push.example.com/abc"); err != nil {
		t.Fatalf("DeletePushSubscriptionByEndpoint = %v, want nil", err)
	}
	if len(captured.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(captured.queries))
	}
	if !strings.Contains(captured.queries[0], "user_id = $2") {
		t.Fatalf("delete must be scoped by user; query:\n%s", captured.queries[0])
	}
	wantArgs := []any{"tenant-a", "user-a", "https://push.example.com/abc"}
	if got := captured.args[0]; !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("delete args = %#v, want %#v", got, wantArgs)
	}
}
