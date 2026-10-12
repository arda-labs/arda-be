package service

import (
	"database/sql"
	"log/slog"

	"github.com/arda-labs/arda/libs/go/arda-events/outbox"
	"github.com/nats-io/nats.go"
)

// workflowOutboxTable carries case and task events; each row stores its subject.
var workflowOutboxTable = outbox.Table{
	Name:                    "workflow_outbox",
	SubjectColumn:           "subject",
	ClearLastErrorOnPublish: true,
}

// NewWorkflowOutboxRelay returns nil when NATS is unavailable; events stay
// pending in workflow_outbox until a relay runs.
func NewWorkflowOutboxRelay(db *sql.DB, conn *nats.Conn, logger *slog.Logger) *outbox.Relay {
	return outbox.FromConn(db, conn, workflowOutboxTable, logger)
}
