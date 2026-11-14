// Package db owns the Postgres connection pool and the schema migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", used by goose
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return pool, nil
}

// Migrate applies every pending migration and returns how many it applied.
// Running it again on an up-to-date database applies none.
func Migrate(ctx context.Context, url string) (int, error) {
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return 0, fmt.Errorf("open database: %w", err)
	}
	defer sqlDB.Close()

	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return 0, err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, dir)
	if err != nil {
		return 0, fmt.Errorf("load migrations: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrate: %w", err)
	}
	return len(results), nil
}
