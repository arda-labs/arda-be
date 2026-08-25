package migrations

import "embed"

// FS contains the AI service's service-owned PostgreSQL migrations.
//
//go:embed *.sql
var FS embed.FS
