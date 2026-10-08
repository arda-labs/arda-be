package migrations

import "embed"

// Canary route migrations live under canary/ and are applied only after the
// candidate service and its workers are ready; startup Goose migrations stay
// limited to schema changes.
//go:embed *.sql
var FS embed.FS
