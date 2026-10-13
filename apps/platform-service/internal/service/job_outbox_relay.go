package service

import (
	"database/sql"
	"log/slog"

	"github.com/arda-labs/arda/libs/go/arda-events/outbox"
	"github.com/nats-io/nats.go"
)

// platformOutboxTable carries scheduled-job failure/recovery events; each row
// stores its subject. last_error is kept after a successful publish, as the
// previous in-package relay did.
var platformOutboxTable = outbox.Table{
	Name:          "plt_outbox",
	SubjectColumn: "subject",
}

// NewJobOutboxRelay returns nil when NATS is unavailable; events stay pending
// in plt_outbox until a relay runs.
func NewJobOutboxRelay(db *sql.DB, conn *nats.Conn, logger *slog.Logger) *outbox.Relay {
	return outbox.FromConn(db, conn, platformOutboxTable, logger)
}
