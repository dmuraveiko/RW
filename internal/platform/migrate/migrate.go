package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func Up(ctx context.Context, databaseURL, directory string) error {
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer database.Close()
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, database, os.DirFS(directory), goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err = provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func CheckVersion(ctx context.Context, pool *pgxpool.Pool, expected int64) error {
	var version int64
	err := pool.QueryRow(ctx, `select coalesce(max(version_id) filter (where is_applied), 0) from goose_db_version`).Scan(&version)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version != expected {
		return fmt.Errorf("schema version mismatch: expected %d, got %d", expected, version)
	}
	return nil
}

var ErrInvalidService = errors.New("service must be bot or sessions")
