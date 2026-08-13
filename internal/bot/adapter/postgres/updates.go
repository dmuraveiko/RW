package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UpdateStore struct {
	pool      *pgxpool.Pool
	transport string
}

func NewUpdateStore(pool *pgxpool.Pool, transport string) *UpdateStore {
	return &UpdateStore{pool: pool, transport: transport}
}

func (s *UpdateStore) Begin(ctx context.Context, updateID int64, payloadDigest []byte) (bool, error) {
	command, err := s.pool.Exec(ctx, `
		INSERT INTO telegram_updates (transport, update_id, status, payload_digest)
		VALUES ($1, $2, 'PROCESSING', $3)
		ON CONFLICT (transport, update_id) DO NOTHING`, s.transport, updateID, payloadDigest)
	if err != nil {
		return false, fmt.Errorf("insert update: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

func (s *UpdateStore) Finish(ctx context.Context, updateID int64, outcome string) error {
	command, err := s.pool.Exec(ctx, `
		UPDATE telegram_updates
		SET status = $3, processed_at = now()
		WHERE transport = $1 AND update_id = $2 AND status = 'PROCESSING'`, s.transport, updateID, outcome)
	if err != nil {
		return fmt.Errorf("update outcome: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("update %d is not in processing state", updateID)
	}
	return nil
}
