package message

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	platformcrypto "github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrMessageIDCollision = errors.New("message ID already exists with a different payload")

type InboxResult struct {
	Accepted        bool
	Completed       bool
	ResultMessageID *uuid.UUID
}

type OutboxMessage struct {
	MessageID uuid.UUID
	Subject   string
	Envelope  []byte
	Kind      string
	ExpiresAt time.Time
}

func RegisterInbox(ctx context.Context, tx pgx.Tx, messageID uuid.UUID, subject, producer string, payloadDigest []byte, expiresAt time.Time) (InboxResult, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO message_inbox (message_id, subject, producer, payload_digest, status, expires_at)
		VALUES ($1, $2, $3, $4, 'PROCESSING', $5)
		ON CONFLICT (message_id) DO NOTHING`, messageID, subject, producer, payloadDigest, expiresAt)
	if err != nil {
		return InboxResult{}, fmt.Errorf("insert inbox message: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return InboxResult{Accepted: true}, nil
	}
	var storedDigest []byte
	var status string
	var resultID *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT payload_digest, status, result_message_id FROM message_inbox WHERE message_id = $1`, messageID).Scan(&storedDigest, &status, &resultID)
	if err != nil {
		return InboxResult{}, fmt.Errorf("read duplicate inbox message: %w", err)
	}
	if !bytes.Equal(storedDigest, payloadDigest) {
		return InboxResult{}, ErrMessageIDCollision
	}
	return InboxResult{Completed: status == "COMPLETED", ResultMessageID: resultID}, nil
}

func CompleteInbox(ctx context.Context, tx pgx.Tx, messageID, resultMessageID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE message_inbox
		SET status = 'COMPLETED', result_message_id = $2, completed_at = now()
		WHERE message_id = $1 AND status = 'PROCESSING'`, messageID, resultMessageID)
	if err != nil {
		return fmt.Errorf("complete inbox message: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("inbox message is not processing")
	}
	return nil
}

func RejectInbox(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE message_inbox
		SET status = 'REJECTED', completed_at = now()
		WHERE message_id = $1 AND status = 'PROCESSING'`, messageID)
	if err != nil {
		return fmt.Errorf("reject inbox message: %w", err)
	}
	return nil
}

func AcknowledgeInbox(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE message_inbox
		SET status = 'APPLIED', completed_at = now()
		WHERE message_id = $1 AND status = 'PROCESSING'`, messageID)
	if err != nil {
		return fmt.Errorf("acknowledge inbox message: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("inbox message is not processing")
	}
	return nil
}

func InsertOutbox(ctx context.Context, tx pgx.Tx, item OutboxMessage) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO message_outbox (message_id, subject, envelope, kind, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (message_id) DO NOTHING`, item.MessageID, item.Subject, item.Envelope, item.Kind, item.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert outbox message: %w", err)
	}
	return nil
}

func SealOutbox(item OutboxMessage, keyring platformcrypto.DataKeyring, service string) (OutboxMessage, error) {
	sealed, err := keyring.Encrypt(item.Envelope, outboxAAD(service, item.MessageID, item.Subject))
	if err != nil {
		return item, fmt.Errorf("encrypt outbox envelope: %w", err)
	}
	item.Envelope, err = json.Marshal(sealed)
	if err != nil {
		return item, fmt.Errorf("encode outbox ciphertext: %w", err)
	}
	return item, nil
}

func OpenOutbox(item OutboxMessage, keyring platformcrypto.DataKeyring, service string) (OutboxMessage, error) {
	var sealed platformcrypto.Ciphertext
	if err := json.Unmarshal(item.Envelope, &sealed); err != nil {
		return item, fmt.Errorf("decode outbox ciphertext: %w", err)
	}
	plain, err := keyring.Decrypt(sealed, outboxAAD(service, item.MessageID, item.Subject))
	if err != nil {
		return item, fmt.Errorf("decrypt outbox envelope: %w", err)
	}
	item.Envelope = plain
	return item, nil
}

func outboxAAD(service string, messageID uuid.UUID, subject string) []byte {
	return []byte(service + "\x00message_outbox\x00" + messageID.String() + "\x00" + subject + "\x00envelope")
}

func RequeueOutbox(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE message_outbox
		SET status = 'PENDING', next_attempt_at = now(), lease_until = NULL
		WHERE message_id = $1 AND status NOT IN ('CONFIRMED', 'DEAD')`, messageID)
	if err != nil {
		return fmt.Errorf("requeue outbox message: %w", err)
	}
	return nil
}

func ConfirmOutbox(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE message_outbox
		SET status = 'CONFIRMED', confirmed_at = now(), lease_until = NULL
		WHERE message_id = $1 AND status <> 'DEAD'`, messageID)
	if err != nil {
		return fmt.Errorf("confirm outbox message: %w", err)
	}
	return nil
}
