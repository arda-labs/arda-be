package service

import (
	"database/sql"
	"log/slog"

	"github.com/arda-labs/arda/libs/go/arda-events/outbox"
	"github.com/nats-io/nats.go"
)

// reportingOutboxTable carries indicator-breach alerts. last_error is NOT NULL
// in rpt_outbox_events, so a published row keeps its last failure text.
var reportingOutboxTable = outbox.Table{
	Name:          "rpt_outbox_events",
	SubjectColumn: "subject",
}

// NewOutboxRelay returns nil when NATS is unavailable: the reporting ETL and
// the alert records keep working without an event bus, and a NATS outage
// delays an alert but never drops it, because the alert row itself is already
// durable in rpt_indicator_alerts.
func NewOutboxRelay(db *sql.DB, conn *nats.Conn, logger *slog.Logger) *outbox.Relay {
	return outbox.FromConn(db, conn, reportingOutboxTable, logger)
}
