package service

import (
	"database/sql"
	"log/slog"

	"github.com/arda-labs/arda/libs/go/arda-events/outbox"
	"github.com/nats-io/nats.go"
)

// loanOutboxTable carries disbursement decision events; each row stores its
// subject.
var loanOutboxTable = outbox.Table{
	Name:                    "loan_outbox",
	SubjectColumn:           "subject",
	ClearLastErrorOnPublish: true,
}

// NewDisbursementOutboxRelay returns nil when NATS is unavailable; events stay
// pending in loan_outbox until a relay runs.
func NewDisbursementOutboxRelay(db *sql.DB, conn *nats.Conn, logger *slog.Logger) *outbox.Relay {
	return outbox.FromConn(db, conn, loanOutboxTable, logger)
}
