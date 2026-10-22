package database

import (
	"github.com/happyro/happyro-api/migrations"
	"io/fs"
)

func migrationsSub() (fs.FS, error) { return fs.Sub(migrations.Files, "management") }
