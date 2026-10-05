// Package migrations embeds versioned SQL into the migration executable.
package migrations

import "embed"

// FS is shared by the runner and integration tests.
//
//go:embed *.sql
var FS embed.FS

// Version must change together with the newest migration and readiness contract.
const Version int64 = 1
