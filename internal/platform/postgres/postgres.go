package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/dmuraveiko/RW/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, cfg config.Database, logger *slog.Logger) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, errors.New("parse database configuration")
	}
	poolConfig.MaxConns = cfg.PoolMax
	poolConfig.MinConns = cfg.PoolMin
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = durationMilliseconds(cfg.StatementTimeout)
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = durationMilliseconds(cfg.LockTimeout)
	poolConfig.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = durationMilliseconds(cfg.IdleTxTimeout)
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "realwallet"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	var version int
	if err = pool.QueryRow(connectCtx, "select current_setting('server_version_num')::integer").Scan(&version); err != nil {
		pool.Close()
		return nil, fmt.Errorf("read database version: %w", err)
	}
	if version/10000 != 18 {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL major version 18 is required, got %d", version/10000)
	}
	logger.Info("database connection established", "postgres_major", version/10000)
	return pool, nil
}

func durationMilliseconds(value time.Duration) string {
	return strconv.FormatInt(value.Milliseconds(), 10)
}
