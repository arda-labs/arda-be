package service

import (
	"database/sql"
	"log/slog"

	ardaevents "github.com/arda-labs/arda/libs/go/arda-events"
	"github.com/arda-labs/arda/libs/go/arda-events/outbox"
	"github.com/nats-io/nats.go"
)

// financeOutboxTable publishes every fin_outbox row on the registry subject
// for posted transactions.
var financeOutboxTable = outbox.Table{
	Name:                    "fin_outbox",
	FixedSubject:            ardaevents.SubjectFinanceTransactionPosted,
	ClearLastErrorOnPublish: true,
}

// NewOutboxRelay returns nil when NATS is unavailable; postings stay pending
// in fin_outbox until a relay runs.
func NewOutboxRelay(db *sql.DB, conn *nats.Conn, logger *slog.Logger) *outbox.Relay {
	return outbox.FromConn(db, conn, financeOutboxTable, logger)
}
