package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/dmuraveiko/RW/internal/platform/message"
	"github.com/dmuraveiko/RW/internal/sessions/contract"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool           *pgxpool.Pool
	keyring        crypto.DataKeyring
	fingerprintKey []byte
}

var ErrActivationConflict = errors.New("activation conflicts with an existing active session")

type Incoming struct {
	MessageID uuid.UUID
	Subject   string
	Producer  string
	Payload   []byte
	ExpiresAt time.Time
}

type Completion struct {
	OperationID      uuid.UUID
	SessionID        uuid.UUID
	BalanceID        string
	Activated        bool
	AuthorityVersion int64
	Code             string
	Retryable        bool
	OccurredAt       time.Time
}

type PendingRetry struct {
	VerificationID        uuid.UUID
	OperationID           uuid.UUID
	CommandMessageID      uuid.UUID
	TopupCommandMessageID uuid.UUID
	OfferExpiresAt        time.Time
	BalanceID             string
	Facts                 contract.TopupVerify
}

type ResultFactory func(Completion) (message.OutboxMessage, error)

func NewStore(pool *pgxpool.Pool, keyring crypto.DataKeyring, fingerprintKey []byte) *Store {
	return &Store{pool: pool, keyring: keyring, fingerprintKey: fingerprintKey}
}

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) Keyring() crypto.DataKeyring { return s.keyring }

func (s *Store) AcceptVerification(ctx context.Context, incoming Incoming, command contract.ActivationVerify, topup message.OutboxMessage) error {
	operationID, _ := uuid.Parse(command.OperationID)
	sessionID, _ := uuid.Parse(command.SessionID)
	verificationID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate verification ID: %w", err)
	}
	balanceCiphertext, err := s.encrypt("activation_verifications", verificationID, "balance_id", command.BalanceID)
	if err != nil {
		return err
	}
	senderCiphertext, err := s.encrypt("activation_verifications", verificationID, "sender_wallet", command.SenderWallet)
	if err != nil {
		return err
	}
	receiverCiphertext, err := s.encrypt("activation_verifications", verificationID, "receiver_wallet", command.ReceiverWallet)
	if err != nil {
		return err
	}
	offerExpires, _ := time.Parse(time.RFC3339Nano, command.OfferExpiresAt)
	digest := sha256.Sum256(incoming.Payload)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin accept verification: %w", err)
	}
	defer tx.Rollback(ctx)
	inbox, err := message.RegisterInbox(ctx, tx, incoming.MessageID, incoming.Subject, incoming.Producer, digest[:], incoming.ExpiresAt)
	if err != nil {
		return err
	}
	if !inbox.Accepted {
		if inbox.Completed && inbox.ResultMessageID != nil {
			if err = message.RequeueOutbox(ctx, tx, *inbox.ResultMessageID); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	if err = s.insertOutbox(ctx, tx, topup); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO activation_verifications (
			id, operation_id, command_message_id, topup_command_message_id, session_id,
			balance_id_ciphertext, balance_id_fingerprint, bot_id, telegram_user_id, telegram_chat_id,
			sender_wallet_ciphertext, sender_wallet_fingerprint, receiver_wallet_ciphertext, display_label,
			receiver_wallet_fingerprint, amount, network, transaction_id, external_reservation_id,
			offer_expires_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULLIF($14, ''), $15, $16, $17, $18, $19, $20, 'PENDING')`,
		verificationID, operationID, incoming.MessageID, topup.MessageID, sessionID,
		balanceCiphertext, s.fingerprint(command.BalanceID), command.BotID, command.TelegramUserID, command.TelegramChatID,
		senderCiphertext, s.fingerprint(command.SenderWallet), receiverCiphertext, command.DisplayLabel, s.fingerprint(command.ReceiverWallet),
		command.Amount, command.Network, command.TransactionID, command.ExternalReservationID, offerExpires)
	if err != nil {
		return fmt.Errorf("insert activation verification: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit activation verification: %w", err)
	}
	return nil
}

func (s *Store) ApplyTopupVerified(ctx context.Context, incoming Incoming, verified contract.TopupVerified, factory ResultFactory) error {
	operationID, err := uuid.Parse(verified.OperationID)
	if err != nil {
		return errors.New("invalid operation ID")
	}
	digest := sha256.Sum256(incoming.Payload)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin verified activation: %w", err)
	}
	defer tx.Rollback(ctx)
	inbox, err := message.RegisterInbox(ctx, tx, incoming.MessageID, incoming.Subject, incoming.Producer, digest[:], incoming.ExpiresAt)
	if err != nil {
		return err
	}
	if !inbox.Accepted {
		if inbox.Completed && inbox.ResultMessageID != nil {
			if err = message.RequeueOutbox(ctx, tx, *inbox.ResultMessageID); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	row, err := s.lockVerification(ctx, tx, operationID)
	if err != nil {
		return err
	}
	if row.Status != "PENDING" {
		if err = message.RejectInbox(ctx, tx, incoming.MessageID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	expected, err := s.expectedFacts(row)
	if err != nil {
		return err
	}
	completion := Completion{OperationID: operationID, SessionID: row.SessionID, OccurredAt: time.Now().UTC()}
	finalizedAt, finalizedErr := time.Parse(time.RFC3339Nano, verified.FinalizedAt)
	if !sameFacts(verified, expected) || finalizedErr != nil || finalizedAt.After(row.OfferExpiresAt) || finalizedAt.After(time.Now().UTC().Add(2*time.Minute)) {
		completion.Code = "CONTRACT_VIOLATION"
		return s.completeRejected(ctx, tx, incoming.MessageID, row, completion, factory)
	}
	balanceID, err := s.decrypt("activation_verifications", row.ID, "balance_id", row.BalanceCiphertext)
	if err != nil {
		return err
	}
	senderWallet, err := s.decrypt("activation_verifications", row.ID, "sender_wallet", row.SenderCiphertext)
	if err != nil {
		return err
	}
	completion.BalanceID = balanceID
	identityID, walletMatch, err := s.ensureIdentity(ctx, tx, row, balanceID, senderWallet)
	if err != nil {
		return err
	}
	if !walletMatch {
		completion.Code = "WALLET_MISMATCH"
		return s.completeRejected(ctx, tx, incoming.MessageID, row, completion, factory)
	}
	authorityVersion, err := s.activateSession(ctx, tx, row, identityID, completion.OccurredAt)
	if errors.Is(err, ErrActivationConflict) {
		completion.Code = "CONFLICT"
		return s.completeRejected(ctx, tx, incoming.MessageID, row, completion, factory)
	}
	if err != nil {
		return err
	}
	completion.Activated = true
	completion.AuthorityVersion = authorityVersion
	result, err := factory(completion)
	if err != nil {
		return err
	}
	result.Kind = "RESULT"
	if err = s.insertOutbox(ctx, tx, result); err != nil {
		return err
	}
	if err = s.finishVerification(ctx, tx, row, "VERIFIED", ""); err != nil {
		return err
	}
	if err = message.CompleteInbox(ctx, tx, row.CommandMessageID, result.MessageID); err != nil {
		return err
	}
	if err = message.CompleteInbox(ctx, tx, incoming.MessageID, result.MessageID); err != nil {
		return err
	}
	if err = message.ConfirmOutbox(ctx, tx, row.TopupCommandMessageID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit verified activation: %w", err)
	}
	return nil
}

func (s *Store) ApplyTopupRejected(ctx context.Context, incoming Incoming, rejection contract.Rejection, factory ResultFactory) error {
	operationID, err := uuid.Parse(rejection.OperationID)
	if err != nil {
		return errors.New("invalid operation ID")
	}
	digest := sha256.Sum256(incoming.Payload)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	inbox, err := message.RegisterInbox(ctx, tx, incoming.MessageID, incoming.Subject, incoming.Producer, digest[:], incoming.ExpiresAt)
	if err != nil {
		return err
	}
	if !inbox.Accepted {
		if inbox.Completed && inbox.ResultMessageID != nil {
			if err = message.RequeueOutbox(ctx, tx, *inbox.ResultMessageID); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	row, err := s.lockVerification(ctx, tx, operationID)
	if err != nil {
		return err
	}
	if row.Status != "PENDING" {
		if err = message.RejectInbox(ctx, tx, incoming.MessageID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	balanceID, err := s.decrypt("activation_verifications", row.ID, "balance_id", row.BalanceCiphertext)
	if err != nil {
		return err
	}
	completion := Completion{OperationID: operationID, SessionID: row.SessionID, BalanceID: balanceID, Code: rejection.Code, Retryable: rejection.Retryable, OccurredAt: time.Now().UTC()}
	return s.completeRejected(ctx, tx, incoming.MessageID, row, completion, factory)
}

func (s *Store) PendingRetries(ctx context.Context, limit int) ([]PendingRetry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT verification.id, verification.operation_id, verification.command_message_id,
		       verification.topup_command_message_id, verification.offer_expires_at,
		       verification.balance_id_ciphertext, verification.sender_wallet_ciphertext,
		       verification.receiver_wallet_ciphertext, verification.amount, verification.network,
		       verification.transaction_id, verification.external_reservation_id
		FROM activation_verifications AS verification
		JOIN message_outbox AS outbox ON outbox.message_id = verification.topup_command_message_id
		WHERE verification.status = 'PENDING'
		  AND (verification.offer_expires_at <= now() OR outbox.status = 'DEAD')
		ORDER BY verification.updated_at, verification.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending verification retries: %w", err)
	}
	defer rows.Close()
	var result []PendingRetry
	for rows.Next() {
		var item PendingRetry
		var balanceCiphertext, senderCiphertext, receiverCiphertext []byte
		if err = rows.Scan(&item.VerificationID, &item.OperationID, &item.CommandMessageID, &item.TopupCommandMessageID, &item.OfferExpiresAt, &balanceCiphertext, &senderCiphertext, &receiverCiphertext, &item.Facts.Amount, &item.Facts.Network, &item.Facts.TransactionID, &item.Facts.ExternalReservationID); err != nil {
			return nil, fmt.Errorf("scan pending verification retry: %w", err)
		}
		item.Facts.OperationID = item.OperationID.String()
		item.Facts.Asset = "USDT"
		if item.BalanceID, err = s.decrypt("activation_verifications", item.VerificationID, "balance_id", balanceCiphertext); err != nil {
			return nil, err
		}
		if item.Facts.SenderWallet, err = s.decrypt("activation_verifications", item.VerificationID, "sender_wallet", senderCiphertext); err != nil {
			return nil, err
		}
		if item.Facts.ReceiverWallet, err = s.decrypt("activation_verifications", item.VerificationID, "receiver_wallet", receiverCiphertext); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ReplaceTopupCommand(ctx context.Context, item PendingRetry, replacement message.OutboxMessage) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var current uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT status, topup_command_message_id FROM activation_verifications WHERE id = $1 FOR UPDATE`, item.VerificationID).Scan(&status, &current); err != nil {
		return err
	}
	if status != "PENDING" || current != item.TopupCommandMessageID {
		return tx.Commit(ctx)
	}
	if err = s.insertOutbox(ctx, tx, replacement); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE activation_verifications SET topup_command_message_id = $2, updated_at = now(), version = version + 1 WHERE id = $1`, item.VerificationID, replacement.MessageID)
	if err != nil {
		return fmt.Errorf("replace top-up verification command: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Store) ExpireVerification(ctx context.Context, item PendingRetry, factory ResultFactory) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var current uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT status, topup_command_message_id FROM activation_verifications WHERE id = $1 FOR UPDATE`, item.VerificationID).Scan(&status, &current); err != nil {
		return err
	}
	if status != "PENDING" || current != item.TopupCommandMessageID {
		return tx.Commit(ctx)
	}
	completion := Completion{OperationID: item.OperationID, BalanceID: item.BalanceID, Code: "EXPIRED", OccurredAt: time.Now().UTC()}
	var sessionID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT session_id FROM activation_verifications WHERE id = $1`, item.VerificationID).Scan(&sessionID); err != nil {
		return err
	}
	completion.SessionID = sessionID
	result, err := factory(completion)
	if err != nil {
		return err
	}
	result.Kind = "RESULT"
	if err = s.insertOutbox(ctx, tx, result); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE activation_verifications SET status = 'EXPIRED', failure_code = 'EXPIRED', completed_at = now(), updated_at = now(), version = version + 1 WHERE id = $1`, item.VerificationID)
	if err != nil {
		return err
	}
	if err = message.CompleteInbox(ctx, tx, item.CommandMessageID, result.MessageID); err != nil {
		return err
	}
	if err = message.ConfirmOutbox(ctx, tx, current); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type verificationRow struct {
	ID                    uuid.UUID
	CommandMessageID      uuid.UUID
	TopupCommandMessageID uuid.UUID
	SessionID             uuid.UUID
	BalanceCiphertext     []byte
	BalanceFingerprint    []byte
	BotID                 int64
	TelegramUserID        int64
	TelegramChatID        int64
	SenderCiphertext      []byte
	SenderFingerprint     []byte
	ReceiverCiphertext    []byte
	DisplayLabel          *string
	Amount                string
	Network               string
	TransactionID         string
	ExternalReservationID string
	OfferExpiresAt        time.Time
	Status                string
}

func (s *Store) lockVerification(ctx context.Context, tx pgx.Tx, operationID uuid.UUID) (verificationRow, error) {
	var row verificationRow
	err := tx.QueryRow(ctx, `
		SELECT id, command_message_id, topup_command_message_id, session_id,
		       balance_id_ciphertext, balance_id_fingerprint, bot_id, telegram_user_id, telegram_chat_id,
		       sender_wallet_ciphertext, sender_wallet_fingerprint, receiver_wallet_ciphertext, display_label,
		       amount, network, transaction_id, external_reservation_id, offer_expires_at, status
		FROM activation_verifications WHERE operation_id = $1 FOR UPDATE`, operationID).Scan(
		&row.ID, &row.CommandMessageID, &row.TopupCommandMessageID, &row.SessionID,
		&row.BalanceCiphertext, &row.BalanceFingerprint, &row.BotID, &row.TelegramUserID, &row.TelegramChatID,
		&row.SenderCiphertext, &row.SenderFingerprint, &row.ReceiverCiphertext, &row.DisplayLabel,
		&row.Amount, &row.Network, &row.TransactionID, &row.ExternalReservationID, &row.OfferExpiresAt, &row.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errors.New("activation verification not found")
	}
	if err != nil {
		return row, fmt.Errorf("lock activation verification: %w", err)
	}
	return row, nil
}

func (s *Store) expectedFacts(row verificationRow) (contract.TopupVerify, error) {
	sender, err := s.decrypt("activation_verifications", row.ID, "sender_wallet", row.SenderCiphertext)
	if err != nil {
		return contract.TopupVerify{}, err
	}
	receiver, err := s.decrypt("activation_verifications", row.ID, "receiver_wallet", row.ReceiverCiphertext)
	if err != nil {
		return contract.TopupVerify{}, err
	}
	return contract.TopupVerify{OperationID: "", TransactionID: row.TransactionID, ExternalReservationID: row.ExternalReservationID, SenderWallet: sender, ReceiverWallet: receiver, Amount: row.Amount, Asset: "USDT", Network: row.Network}, nil
}

func sameFacts(actual contract.TopupVerified, expected contract.TopupVerify) bool {
	return actual.TransactionID == expected.TransactionID && actual.ExternalReservationID == expected.ExternalReservationID && actual.SenderWallet == expected.SenderWallet && actual.ReceiverWallet == expected.ReceiverWallet && actual.Amount == expected.Amount && actual.Network == expected.Network
}

func (s *Store) ensureIdentity(ctx context.Context, tx pgx.Tx, row verificationRow, balanceID, senderWallet string) (uuid.UUID, bool, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(encode($1::bytea, 'hex'), 0))`, row.BalanceFingerprint); err != nil {
		return uuid.Nil, false, fmt.Errorf("lock balance identity: %w", err)
	}
	var identityID uuid.UUID
	var storedWalletCiphertext, storedWalletFingerprint []byte
	var storedNetwork string
	err := tx.QueryRow(ctx, `SELECT id, sender_wallet_ciphertext, sender_wallet_fingerprint, network FROM balance_identities WHERE balance_id_fingerprint = $1 FOR UPDATE`, row.BalanceFingerprint).Scan(&identityID, &storedWalletCiphertext, &storedWalletFingerprint, &storedNetwork)
	if errors.Is(err, pgx.ErrNoRows) {
		identityID, err = uuid.NewV7()
		if err != nil {
			return uuid.Nil, false, err
		}
		balanceCiphertext, encryptErr := s.encrypt("balance_identities", identityID, "balance_id", balanceID)
		if encryptErr != nil {
			return uuid.Nil, false, encryptErr
		}
		walletCiphertext, encryptErr := s.encrypt("balance_identities", identityID, "sender_wallet", senderWallet)
		if encryptErr != nil {
			return uuid.Nil, false, encryptErr
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO balance_identities (id, balance_id_ciphertext, balance_id_fingerprint, sender_wallet_ciphertext, sender_wallet_fingerprint, network)
			VALUES ($1, $2, $3, $4, $5, $6)`, identityID, balanceCiphertext, row.BalanceFingerprint, walletCiphertext, row.SenderFingerprint, row.Network)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("insert balance identity: %w", err)
		}
		return identityID, true, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("read balance identity: %w", err)
	}
	if storedNetwork != row.Network || !hmac.Equal(storedWalletFingerprint, row.SenderFingerprint) {
		return identityID, false, nil
	}
	storedWallet, err := s.decrypt("balance_identities", identityID, "sender_wallet", storedWalletCiphertext)
	if err != nil {
		return uuid.Nil, false, err
	}
	return identityID, hmac.Equal([]byte(storedWallet), []byte(senderWallet)), nil
}

func (s *Store) activateSession(ctx context.Context, tx pgx.Tx, row verificationRow, identityID uuid.UUID, now time.Time) (int64, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fmt.Sprintf("telegram:%d:%d", row.BotID, row.TelegramUserID)); err != nil {
		return 0, fmt.Errorf("lock Telegram identity: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "transaction:"+row.Network+":"+row.TransactionID); err != nil {
		return 0, fmt.Errorf("lock activation transaction: %w", err)
	}
	var conflictingSession uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM active_sessions
		WHERE ((client_type = 'TELEGRAM' AND bot_id = $1 AND telegram_user_id = $2 AND status = 'ACTIVE')
		       OR (activation_network = $3 AND activation_transaction_id = $4))
		  AND id <> $5
		LIMIT 1`, row.BotID, row.TelegramUserID, row.Network, row.TransactionID, row.SessionID).Scan(&conflictingSession)
	if err == nil {
		return 0, ErrActivationConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("check activation conflicts: %w", err)
	}
	var currentIdentity uuid.UUID
	var currentStatus string
	var currentVersion int64
	err = tx.QueryRow(ctx, `SELECT balance_identity_id, status, version FROM active_sessions WHERE id = $1 FOR UPDATE`, row.SessionID).Scan(&currentIdentity, &currentStatus, &currentVersion)
	eventType := "ACTIVATED"
	if errors.Is(err, pgx.ErrNoRows) {
		currentVersion = 1
		_, err = tx.Exec(ctx, `
			INSERT INTO active_sessions (id, balance_identity_id, client_type, bot_id, telegram_user_id, telegram_chat_id, display_label,
				status, activation_network, activation_transaction_id, first_activated_at, last_activated_at)
			VALUES ($1, $2, 'TELEGRAM', $3, $4, $5, $6, 'ACTIVE', $7, $8, $9, $9)`,
			row.SessionID, identityID, row.BotID, row.TelegramUserID, row.TelegramChatID, row.DisplayLabel, row.Network, row.TransactionID, now)
	} else if err != nil {
		return 0, fmt.Errorf("read active session: %w", err)
	} else {
		if currentIdentity != identityID || currentStatus != "ACTIVE" {
			return 0, ErrActivationConflict
		}
		eventType = "REACTIVATED"
		currentVersion++
		_, err = tx.Exec(ctx, `
			UPDATE active_sessions
			SET telegram_chat_id = $2, activation_network = $3, activation_transaction_id = $4,
			    last_activated_at = $5, updated_at = now(), version = $6
			WHERE id = $1`, row.SessionID, row.TelegramChatID, row.Network, row.TransactionID, now, currentVersion)
	}
	if err != nil {
		return 0, fmt.Errorf("persist active session: %w", err)
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO session_events (id, session_id, operation_id, event_type, authority_version, occurred_at, details)
		VALUES ($1, $2, (SELECT operation_id FROM activation_verifications WHERE id = $3), $4, $5, $6, jsonb_build_object('network', $7::text, 'transaction_id', $8::text))`,
		eventID, row.SessionID, row.ID, eventType, currentVersion, now, row.Network, row.TransactionID)
	if err != nil {
		return 0, fmt.Errorf("insert session event: %w", err)
	}
	return currentVersion, nil
}

func (s *Store) completeRejected(ctx context.Context, tx pgx.Tx, incomingMessageID uuid.UUID, row verificationRow, completion Completion, factory ResultFactory) error {
	result, err := factory(completion)
	if err != nil {
		return err
	}
	result.Kind = "RESULT"
	if err = s.insertOutbox(ctx, tx, result); err != nil {
		return err
	}
	if err = s.finishVerification(ctx, tx, row, "REJECTED", completion.Code); err != nil {
		return err
	}
	if err = message.CompleteInbox(ctx, tx, row.CommandMessageID, result.MessageID); err != nil {
		return err
	}
	if err = message.CompleteInbox(ctx, tx, incomingMessageID, result.MessageID); err != nil {
		return err
	}
	if err = message.ConfirmOutbox(ctx, tx, row.TopupCommandMessageID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rejected activation: %w", err)
	}
	return nil
}

func (s *Store) finishVerification(ctx context.Context, tx pgx.Tx, row verificationRow, status, code string) error {
	_, err := tx.Exec(ctx, `
		UPDATE activation_verifications
		SET status = $2, failure_code = NULLIF($3, ''), completed_at = now(), updated_at = now(), version = version + 1
		WHERE id = $1 AND status = 'PENDING'`, row.ID, status, code)
	return err
}

func (s *Store) fingerprint(value string) []byte {
	return crypto.Fingerprint(s.fingerprintKey, []byte(value))
}

func (s *Store) insertOutbox(ctx context.Context, tx pgx.Tx, item message.OutboxMessage) error {
	sealed, err := message.SealOutbox(item, s.keyring, "rw-active-sessions")
	if err != nil {
		return err
	}
	return message.InsertOutbox(ctx, tx, sealed)
}

func (s *Store) encrypt(table string, rowID uuid.UUID, column, value string) ([]byte, error) {
	sealed, err := s.keyring.Encrypt([]byte(value), aad(table, rowID, column))
	if err != nil {
		return nil, fmt.Errorf("encrypt %s: %w", column, err)
	}
	encoded, err := json.Marshal(sealed)
	if err != nil {
		return nil, fmt.Errorf("encode %s ciphertext: %w", column, err)
	}
	return encoded, nil
}

func (s *Store) decrypt(table string, rowID uuid.UUID, column string, encoded []byte) (string, error) {
	var sealed crypto.Ciphertext
	if err := json.Unmarshal(encoded, &sealed); err != nil {
		return "", fmt.Errorf("decode %s ciphertext: %w", column, err)
	}
	plain, err := s.keyring.Decrypt(sealed, aad(table, rowID, column))
	if err != nil {
		return "", fmt.Errorf("decrypt %s: %w", column, err)
	}
	return string(plain), nil
}

func aad(table string, rowID uuid.UUID, column string) []byte {
	return []byte("rw-active-sessions\x00" + table + "\x00" + rowID.String() + "\x00" + column)
}
