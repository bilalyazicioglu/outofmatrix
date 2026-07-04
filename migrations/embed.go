// Package migrations embeds the SQL schema files so the binary can migrate
// its own database at boot (see internal/repository/postgres.Migrate).
// The files are also mounted into docker-entrypoint-initdb.d for fresh
// volumes; every file is written to be idempotent so both paths coexist.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
