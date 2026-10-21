package migrations

import "embed"

//go:embed management/*.sql
var Files embed.FS
