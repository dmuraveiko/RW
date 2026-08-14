package message

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/dmuraveiko/RW/internal/platform/config"
	platformcrypto "github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

type Publisher struct {
	pool    *pgxpool.Pool
	nats    *nats.Conn
	cfg     config.Messaging
	logger  *slog.Logger
	keyring platformcrypto.DataKeyring
	service string
}

type claimedMessage struct {
	ID       uuid.UUID
	Subject  string
	Envelope []byte
	Attempts int
}

func NewPublisher(pool *pgxpool.Pool, connection *nats.Conn, cfg config.Messaging, logger *slog.Logger, keyring platformcrypto.DataKeyring, service string) *Publisher {
	return &Publisher{pool: pool, nats: connection, cfg: cfg, logger: logger, keyring: keyring, service: service}
}

func (p *Publisher) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.cfg.RetryInitial)
	defer ticker.Stop()
	for {
		if err := p.publishBatch(ctx); err != nil && ctx.Err() == nil {
			p.logger.Error("outbox publish batch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (p *Publisher) publishBatch(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `
		UPDATE message_outbox
		SET status = 'DEAD', lease_until = NULL, last_error_code = 'EXPIRED'
		WHERE status NOT IN ('CONFIRMED', 'DEAD') AND expires_at <= now()`); err != nil {
		return fmt.Errorf("expire outbox messages: %w", err)
	}
	items, err := p.claim(ctx)
	if err != nil || len(items) == 0 {
		return err
	}
	semaphore := make(chan struct{}, p.cfg.OutboxConcurrency)
	var group sync.WaitGroup
	for _, item := range items {
		item := item
		semaphore <- struct{}{}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-semaphore }()
			p.publishOne(ctx, item)
		}()
	}
	group.Wait()
	return nil
}

func (p *Publisher) claim(ctx context.Context) ([]claimedMessage, error) {
	rows, err := p.pool.Query(ctx, `
		WITH due AS (
			SELECT message_id
			FROM message_outbox
			WHERE ((status = 'PENDING' AND next_attempt_at <= now())
			       OR (status = 'PUBLISHED' AND kind = 'COMMAND' AND next_attempt_at <= now())
			       OR (status = 'PUBLISHING' AND lease_until <= now()))
			  AND expires_at > now()
			ORDER BY next_attempt_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE message_outbox AS outbox
		SET status = 'PUBLISHING', lease_until = now() + interval '30 seconds'
		FROM due
		WHERE outbox.message_id = due.message_id
		RETURNING outbox.message_id, outbox.subject, outbox.envelope, outbox.attempts`, p.cfg.OutboxBatch)
	if err != nil {
		return nil, fmt.Errorf("claim outbox messages: %w", err)
	}
	defer rows.Close()
	var result []claimedMessage
	for rows.Next() {
		var item claimedMessage
		if err = rows.Scan(&item.ID, &item.Subject, &item.Envelope, &item.Attempts); err != nil {
			return nil, fmt.Errorf("scan outbox message: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (p *Publisher) publishOne(ctx context.Context, item claimedMessage) {
	opened, err := OpenOutbox(OutboxMessage{MessageID: item.ID, Subject: item.Subject, Envelope: item.Envelope}, p.keyring, p.service)
	if err != nil {
		if markErr := p.markRetry(ctx, item, "OUTBOX_DECRYPT_FAILED"); markErr != nil && !errors.Is(markErr, context.Canceled) {
			p.logger.Error("mark outbox retry failed", "message_id", item.ID, "error", markErr)
		}
		return
	}
	err = p.nats.Publish(item.Subject, opened.Envelope)
	if err == nil {
		err = p.nats.FlushTimeout(2 * time.Second)
	}
	if err != nil {
		if markErr := p.markRetry(ctx, item, "NATS_PUBLISH_FAILED"); markErr != nil && !errors.Is(markErr, context.Canceled) {
			p.logger.Error("mark outbox retry failed", "message_id", item.ID, "error", markErr)
		}
		return
	}
	if _, err = p.pool.Exec(ctx, `
		UPDATE message_outbox
		SET status = 'PUBLISHED', attempts = attempts + 1, published_at = now(),
		    next_attempt_at = now() + $2::interval, lease_until = NULL, last_error_code = NULL
		WHERE message_id = $1 AND status = 'PUBLISHING'`, item.ID, intervalLiteral(p.retryDelay(item.Attempts+1))); err != nil && !errors.Is(err, context.Canceled) {
		p.logger.Error("mark outbox published failed", "message_id", item.ID, "error", err)
	}
}

func (p *Publisher) markRetry(ctx context.Context, item claimedMessage, code string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE message_outbox
		SET status = 'PENDING', attempts = attempts + 1, next_attempt_at = now() + $2::interval,
		    lease_until = NULL, last_error_code = $3
		WHERE message_id = $1 AND status = 'PUBLISHING'`, item.ID, intervalLiteral(p.retryDelay(item.Attempts+1)), code)
	return err
}

func (p *Publisher) retryDelay(attempt int) time.Duration {
	power := math.Pow(2, float64(max(attempt-1, 0)))
	delay := time.Duration(float64(p.cfg.RetryInitial) * power)
	if delay > p.cfg.RetryMax || delay < 0 {
		delay = p.cfg.RetryMax
	}
	jitter := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(delay) * jitter)
}

func intervalLiteral(duration time.Duration) string {
	return fmt.Sprintf("%f seconds", duration.Seconds())
}
