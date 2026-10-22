package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/happyro/happyro-api/internal/config"
	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

func Open(ctx context.Context, cfg config.Database) (*sqlx.DB, error) {
	if cfg.Name == "" || cfg.Username == "" {
		return nil, errors.New("database name and username are required")
	}
	db, err := sqlx.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(3 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sqlx.DB) error {
	// All migrations are scoped to the dedicated admin connection. Never run
	// these against the game or game-log schema.
	fs, err := migrationsSub()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectMySQL, db.DB, fs)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
