package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dmuraveiko/RW/internal/bot/contract"
	botdomain "github.com/dmuraveiko/RW/internal/bot/domain"
	"github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/dmuraveiko/RW/internal/platform/message"
	sessionscontract "github.com/dmuraveiko/RW/internal/sessions/contract"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInviteInvalid     = errors.New("invite is invalid")
	ErrSessionNotFound   = errors.New("telegram session is not bound")
	ErrUnexpectedState   = errors.New("telegram session is in an unexpected state")
	ErrActivationMissing = errors.New("activation attempt not found")
)

type FlowStore struct {
	pool           *pgxpool.Pool
	keyring        crypto.DataKeyring
	fingerprintKey []byte
}

type IncomingMessage struct {
	MessageID uuid.UUID
	Subject   string
	Producer  string
	Payload   []byte
	ExpiresAt time.Time
}

type InviteResultFactory func(token string, expiresAt time.Time) (message.OutboxMessage, error)

type ActivationDraft struct {
	OperationID  uuid.UUID
	SessionID    uuid.UUID
	BalanceID    string
	SenderWallet string
	Amount       string
}

type ReserveFactory func(ActivationDraft) (message.OutboxMessage, error)

type VerificationDraft struct {
	OperationID           uuid.UUID
	SessionID             uuid.UUID
	BalanceID             string
	BotID                 int64
	TelegramUserID        int64
	TelegramChatID        int64
	DisplayLabel          string
	SenderWallet          string
	ReceiverWallet        string
	Amount                string
	TransactionID         string
	ExternalReservationID string
	OfferValidFrom        time.Time
	OfferExpiresAt        time.Time
}

type VerifyFactory func(VerificationDraft, uuid.UUID) (message.OutboxMessage, error)

type ConversationState struct {
	Bound       bool
	Active      bool
	SessionID   uuid.UUID
	DialogState string
}

type Notification struct {
	Applied bool
	ChatID  int64
	Text    string
}

func NewFlowStore(pool *pgxpool.Pool, keyring crypto.DataKeyring, fingerprintKey []byte) *FlowStore {
	return &FlowStore{pool: pool, keyring: keyring, fingerprintKey: fingerprintKey}
}

func (s *FlowStore) Pool() *pgxpool.Pool { return s.pool }

func (s *FlowStore) Keyring() crypto.DataKeyring { return s.keyring }

func (s *FlowStore) CreateInvite(ctx context.Context, incoming IncomingMessage, command contract.InviteCreate, ttl time.Duration, factory InviteResultFactory) error {
	inviteID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	token, err := cryptoToken()
	if err != nil {
		return err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	result, err := factory(token, expiresAt)
	if err != nil {
		return err
	}
	result.Kind = "RESULT"
	tokenCiphertext, err := s.encrypt("invites", inviteID, "token", token)
	if err != nil {
		return err
	}
	balanceCiphertext, err := s.encrypt("invites", inviteID, "balance_id", command.BalanceID)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(incoming.Payload)
	tokenDigest := sha256.Sum256([]byte(token))
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
	if err = s.insertOutbox(ctx, tx, result); err != nil {
		return err
	}
	operationID, _ := uuid.Parse(command.OperationID)
	_, err = tx.Exec(ctx, `
		INSERT INTO invites (id, operation_id, command_message_id, result_message_id, token_digest, token_ciphertext,
			balance_id_ciphertext, balance_id_fingerprint, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'ACTIVE', $9)`,
		inviteID, operationID, incoming.MessageID, result.MessageID, tokenDigest[:], tokenCiphertext,
		balanceCiphertext, s.fingerprint(command.BalanceID), expiresAt)
	if err != nil {
		return fmt.Errorf("insert invite: %w", err)
	}
	if err = message.CompleteInbox(ctx, tx, incoming.MessageID, result.MessageID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *FlowStore) ConsumeInvite(ctx context.Context, botID, userID, chatID int64, displayLabel, token string) (uuid.UUID, error) {
	tokenDigest := sha256.Sum256([]byte(token))
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	var existing uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT session_id FROM active_session_projection WHERE bot_id = $1 AND telegram_user_id = $2`, botID, userID).Scan(&existing); err == nil {
		return existing, tx.Commit(ctx)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM inactive_sessions WHERE bot_id = $1 AND telegram_user_id = $2 FOR UPDATE`, botID, userID).Scan(&existing); err == nil {
		return existing, tx.Commit(ctx)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	var inviteID uuid.UUID
	var balanceCiphertext []byte
	var expiresAt time.Time
	var status string
	err = tx.QueryRow(ctx, `SELECT id, balance_id_ciphertext, expires_at, status FROM invites WHERE token_digest = $1 FOR UPDATE`, tokenDigest[:]).Scan(&inviteID, &balanceCiphertext, &expiresAt, &status)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (status != "ACTIVE" || !expiresAt.After(time.Now().UTC())) {
		return uuid.Nil, ErrInviteInvalid
	}
	if err != nil {
		return uuid.Nil, err
	}
	balanceID, err := s.decrypt("invites", inviteID, "balance_id", balanceCiphertext)
	if err != nil {
		return uuid.Nil, err
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	sessionBalance, err := s.encrypt("inactive_sessions", sessionID, "balance_id", balanceID)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO inactive_sessions (id, bot_id, telegram_user_id, telegram_chat_id, balance_id_ciphertext,
			balance_id_fingerprint, display_label, dialog_state, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), 'AWAITING_WALLET', now() + interval '7 days')`,
		sessionID, botID, userID, chatID, sessionBalance, s.fingerprint(balanceID), displayLabel)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert inactive session: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE invites SET status = 'CONSUMED', consumed_at = now(), consumed_by_session_id = $2 WHERE id = $1 AND status = 'ACTIVE'`, inviteID, sessionID)
	if err != nil {
		return uuid.Nil, err
	}
	return sessionID, tx.Commit(ctx)
}

func (s *FlowStore) State(ctx context.Context, botID, userID int64) (ConversationState, error) {
	var state ConversationState
	err := s.pool.QueryRow(ctx, `SELECT session_id FROM active_session_projection WHERE bot_id = $1 AND telegram_user_id = $2`, botID, userID).Scan(&state.SessionID)
	if err == nil {
		state.Bound, state.Active = true, true
		return state, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return state, err
	}
	err = s.pool.QueryRow(ctx, `SELECT id, dialog_state FROM inactive_sessions WHERE bot_id = $1 AND telegram_user_id = $2`, botID, userID).Scan(&state.SessionID, &state.DialogState)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	state.Bound = err == nil
	return state, err
}

func (s *FlowStore) BeginActivation(ctx context.Context, botID, userID int64, wallet, amount, network string, factory ReserveFactory) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sessionID uuid.UUID
	var balanceCiphertext []byte
	var state string
	err = tx.QueryRow(ctx, `
		SELECT id, balance_id_ciphertext, dialog_state FROM inactive_sessions
		WHERE bot_id = $1 AND telegram_user_id = $2 FOR UPDATE`, botID, userID).Scan(&sessionID, &balanceCiphertext, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	if state != "AWAITING_WALLET" && state != "REJECTED" {
		return ErrUnexpectedState
	}
	balanceID, err := s.decrypt("inactive_sessions", sessionID, "balance_id", balanceCiphertext)
	if err != nil {
		return err
	}
	attemptID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	operationID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	draft := ActivationDraft{OperationID: operationID, SessionID: sessionID, BalanceID: balanceID, SenderWallet: wallet, Amount: amount}
	reserve, err := factory(draft)
	if err != nil {
		return err
	}
	reserve.Kind = "COMMAND"
	walletCiphertext, err := s.encrypt("activation_attempts", attemptID, "sender_wallet", wallet)
	if err != nil {
		return err
	}
	if err = s.insertOutbox(ctx, tx, reserve); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO activation_attempts (id, operation_id, session_id, reserve_message_id, sender_wallet_ciphertext,
			sender_wallet_fingerprint, verification_amount, asset, network, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'USDT', $8, 'AWAITING_RESERVATION')`,
		attemptID, operationID, sessionID, reserve.MessageID, walletCiphertext, s.fingerprint(wallet), amount, network)
	if err != nil {
		return fmt.Errorf("insert activation attempt: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE inactive_sessions SET dialog_state = 'AWAITING_RESERVATION', current_attempt_id = $2, updated_at = now(), version = version + 1 WHERE id = $1`, sessionID, attemptID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *FlowStore) ApplyReserved(ctx context.Context, incoming IncomingMessage, result contract.ActivationReserved, network string) (Notification, error) {
	operationID, err := uuid.Parse(result.OperationID)
	if err != nil {
		return Notification{}, err
	}
	digest := sha256.Sum256(incoming.Payload)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Notification{}, err
	}
	defer tx.Rollback(ctx)
	inbox, err := message.RegisterInbox(ctx, tx, incoming.MessageID, incoming.Subject, incoming.Producer, digest[:], incoming.ExpiresAt)
	if err != nil || !inbox.Accepted {
		if err == nil {
			err = tx.Commit(ctx)
		}
		return Notification{}, err
	}
	var attemptID, sessionID, reserveMessageID uuid.UUID
	var amount, status string
	err = tx.QueryRow(ctx, `SELECT id, session_id, reserve_message_id, verification_amount, status FROM activation_attempts WHERE operation_id = $1 FOR UPDATE`, operationID).Scan(&attemptID, &sessionID, &reserveMessageID, &amount, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrActivationMissing
	}
	if err != nil {
		return Notification{}, err
	}
	if status != "AWAITING_RESERVATION" {
		if err = message.RejectInbox(ctx, tx, incoming.MessageID); err != nil {
			return Notification{}, err
		}
		return Notification{}, tx.Commit(ctx)
	}
	if err = botdomain.ValidateReserved(result, operationID.String(), amount, time.Now().UTC()); err != nil {
		return Notification{}, err
	}
	receiverCiphertext, err := s.encrypt("activation_attempts", attemptID, "receiver_wallet", result.ReceiverWallet)
	if err != nil {
		return Notification{}, err
	}
	validFrom, _ := time.Parse(time.RFC3339Nano, result.ValidFrom)
	expiresAt, _ := time.Parse(time.RFC3339Nano, result.ExpiresAt)
	_, err = tx.Exec(ctx, `
		UPDATE activation_attempts SET receiver_wallet_ciphertext = $2, receiver_wallet_fingerprint = $3, external_reservation_id = $4,
			offer_valid_from = $5, offer_expires_at = $6, status = 'AWAITING_PAYMENT', updated_at = now(), version = version + 1
		WHERE id = $1`, attemptID, receiverCiphertext, s.fingerprint(result.ReceiverWallet), result.ExternalReservationID, validFrom, expiresAt)
	if err != nil {
		return Notification{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE inactive_sessions SET dialog_state = 'AWAITING_PAYMENT', updated_at = now(), version = version + 1 WHERE id = $1`, sessionID)
	if err != nil {
		return Notification{}, err
	}
	if err = message.ConfirmOutbox(ctx, tx, reserveMessageID); err != nil {
		return Notification{}, err
	}
	if err = message.AcknowledgeInbox(ctx, tx, incoming.MessageID); err != nil {
		return Notification{}, err
	}
	var chatID int64
	if err = tx.QueryRow(ctx, `SELECT telegram_chat_id FROM inactive_sessions WHERE id = $1`, sessionID).Scan(&chatID); err != nil {
		return Notification{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Notification{}, err
	}
	text := fmt.Sprintf("Реквизиты для активации:\nUSDT, сеть %s\nСумма: %s\nКошелёк получателя: %s\nОплатить до: %s\n\nПереводите только с указанного ранее кошелька.", network, amount, result.ReceiverWallet, expiresAt.UTC().Format(time.RFC3339))
	return Notification{Applied: true, ChatID: chatID, Text: text}, nil
}

func (s *FlowStore) ApplyReserveRejected(ctx context.Context, incoming IncomingMessage, result contract.Rejection) (Notification, error) {
	operationID, err := uuid.Parse(result.OperationID)
	if err != nil {
		return Notification{}, err
	}
	digest := sha256.Sum256(incoming.Payload)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Notification{}, err
	}
	defer tx.Rollback(ctx)
	inbox, err := message.RegisterInbox(ctx, tx, incoming.MessageID, incoming.Subject, incoming.Producer, digest[:], incoming.ExpiresAt)
	if err != nil || !inbox.Accepted {
		if err == nil {
			err = tx.Commit(ctx)
		}
		return Notification{}, err
	}
	var attemptID, sessionID, reserveMessageID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `SELECT id, session_id, reserve_message_id, status FROM activation_attempts WHERE operation_id = $1 FOR UPDATE`, operationID).Scan(&attemptID, &sessionID, &reserveMessageID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrActivationMissing
	}
	if err != nil {
		return Notification{}, err
	}
	if status != "AWAITING_RESERVATION" {
		if err = message.RejectInbox(ctx, tx, incoming.MessageID); err != nil {
			return Notification{}, err
		}
		return Notification{}, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE activation_attempts SET status = 'REJECTED', failure_code = $2, completed_at = now(), updated_at = now(), version = version + 1 WHERE id = $1`, attemptID, result.Code)
	if err != nil {
		return Notification{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE inactive_sessions SET dialog_state = 'REJECTED', updated_at = now(), version = version + 1 WHERE id = $1`, sessionID)
	if err != nil {
		return Notification{}, err
	}
	if err = message.ConfirmOutbox(ctx, tx, reserveMessageID); err != nil {
		return Notification{}, err
	}
	if err = message.AcknowledgeInbox(ctx, tx, incoming.MessageID); err != nil {
		return Notification{}, err
	}
	var chatID int64
	if err = tx.QueryRow(ctx, `SELECT telegram_chat_id FROM inactive_sessions WHERE id = $1`, sessionID).Scan(&chatID); err != nil {
		return Notification{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Notification{}, err
	}
	return Notification{Applied: true, ChatID: chatID, Text: "Сейчас невозможно выдать реквизиты для активации. Попробуйте ещё раз позже."}, nil
}

func (s *FlowStore) ExpireAttempts(ctx context.Context, limit int) ([]Notification, error) {
	rows, err := s.pool.Query(ctx, `
		WITH due AS (
			SELECT attempt.id, attempt.session_id
			FROM activation_attempts AS attempt
			WHERE (attempt.status = 'AWAITING_RESERVATION' AND attempt.created_at <= now() - interval '2 minutes')
			   OR (attempt.status = 'AWAITING_PAYMENT' AND attempt.offer_expires_at <= now())
			   OR (attempt.status = 'AWAITING_VERIFICATION' AND attempt.offer_expires_at + interval '30 minutes' <= now())
			ORDER BY attempt.updated_at, attempt.id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		), expired AS (
			UPDATE activation_attempts AS attempt
			SET status = 'EXPIRED', failure_code = 'EXPIRED', completed_at = now(), updated_at = now(), version = version + 1
			FROM due WHERE attempt.id = due.id
			RETURNING due.session_id
		), sessions AS (
			UPDATE inactive_sessions AS session
			SET dialog_state = 'REJECTED', updated_at = now(), version = version + 1
			FROM expired WHERE session.id = expired.session_id
			RETURNING session.telegram_chat_id
		)
		SELECT telegram_chat_id FROM sessions`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notifications []Notification
	for rows.Next() {
		var chatID int64
		if err = rows.Scan(&chatID); err != nil {
			return nil, err
		}
		notifications = append(notifications, Notification{Applied: true, ChatID: chatID, Text: "Время активации истекло. Отправьте адрес кошелька, чтобы начать новую попытку."})
	}
	return notifications, rows.Err()
}

func (s *FlowStore) ApplyPayment(ctx context.Context, incoming IncomingMessage, result contract.PaymentConfirmed, factory VerifyFactory) (Notification, error) {
	operationID, err := uuid.Parse(result.OperationID)
	if err != nil {
		return Notification{}, err
	}
	digest := sha256.Sum256(incoming.Payload)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Notification{}, err
	}
	defer tx.Rollback(ctx)
	inbox, err := message.RegisterInbox(ctx, tx, incoming.MessageID, incoming.Subject, incoming.Producer, digest[:], incoming.ExpiresAt)
	if err != nil || !inbox.Accepted {
		if err == nil {
			err = tx.Commit(ctx)
		}
		return Notification{}, err
	}
	var attemptID, sessionID uuid.UUID
	var senderCiphertext, receiverCiphertext []byte
	var amount, reservationID, status string
	var validFrom, expiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, session_id, sender_wallet_ciphertext, receiver_wallet_ciphertext, verification_amount,
			external_reservation_id, offer_valid_from, offer_expires_at, status
		FROM activation_attempts WHERE operation_id = $1 FOR UPDATE`, operationID).Scan(
		&attemptID, &sessionID, &senderCiphertext, &receiverCiphertext, &amount, &reservationID, &validFrom, &expiresAt, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrActivationMissing
	}
	if err != nil {
		return Notification{}, err
	}
	if status != "AWAITING_PAYMENT" {
		if err = message.RejectInbox(ctx, tx, incoming.MessageID); err != nil {
			return Notification{}, err
		}
		return Notification{}, tx.Commit(ctx)
	}
	var botID, userID, chatID int64
	var balanceCiphertext []byte
	var displayLabel *string
	if err = tx.QueryRow(ctx, `SELECT bot_id, telegram_user_id, telegram_chat_id, balance_id_ciphertext, display_label FROM inactive_sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&botID, &userID, &chatID, &balanceCiphertext, &displayLabel); err != nil {
		return Notification{}, err
	}
	balanceID, err := s.decrypt("inactive_sessions", sessionID, "balance_id", balanceCiphertext)
	if err != nil {
		return Notification{}, err
	}
	sender, err := s.decrypt("activation_attempts", attemptID, "sender_wallet", senderCiphertext)
	if err != nil {
		return Notification{}, err
	}
	receiver, err := s.decrypt("activation_attempts", attemptID, "receiver_wallet", receiverCiphertext)
	if err != nil {
		return Notification{}, err
	}
	expectedPayment := contract.PaymentConfirmed{OperationID: operationID.String(), ExternalReservationID: reservationID, SenderWallet: sender, ReceiverWallet: receiver, Amount: amount}
	if err = botdomain.ValidatePayment(result, expectedPayment, expiresAt, time.Now().UTC()); err != nil {
		return Notification{}, err
	}
	draft := VerificationDraft{OperationID: operationID, SessionID: sessionID, BalanceID: balanceID, BotID: botID, TelegramUserID: userID, TelegramChatID: chatID, SenderWallet: sender, ReceiverWallet: receiver, Amount: amount, TransactionID: result.TransactionID, ExternalReservationID: reservationID, OfferValidFrom: validFrom, OfferExpiresAt: expiresAt}
	if displayLabel != nil {
		draft.DisplayLabel = *displayLabel
	}
	verify, err := factory(draft, incoming.MessageID)
	if err != nil {
		return Notification{}, err
	}
	verify.Kind = "COMMAND"
	if err = s.insertOutbox(ctx, tx, verify); err != nil {
		return Notification{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE activation_attempts SET verify_message_id = $2, transaction_id = $3, status = 'AWAITING_VERIFICATION', updated_at = now(), version = version + 1 WHERE id = $1`, attemptID, verify.MessageID, result.TransactionID)
	if err != nil {
		return Notification{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE inactive_sessions SET dialog_state = 'AWAITING_VERIFICATION', updated_at = now(), version = version + 1 WHERE id = $1`, sessionID)
	if err != nil {
		return Notification{}, err
	}
	if err = message.AcknowledgeInbox(ctx, tx, incoming.MessageID); err != nil {
		return Notification{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Notification{}, err
	}
	return Notification{Applied: true, ChatID: chatID, Text: "Платёж найден. Выполняется окончательная проверка активации."}, nil
}

func (s *FlowStore) CompleteActivation(ctx context.Context, incoming IncomingMessage, activated *sessionscontract.Activated, rejection *sessionscontract.Rejection) (Notification, error) {
	operationText := ""
	if activated != nil {
		operationText = activated.OperationID
	} else {
		operationText = rejection.OperationID
	}
	operationID, err := uuid.Parse(operationText)
	if err != nil {
		return Notification{}, err
	}
	digest := sha256.Sum256(incoming.Payload)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Notification{}, err
	}
	defer tx.Rollback(ctx)
	inbox, err := message.RegisterInbox(ctx, tx, incoming.MessageID, incoming.Subject, incoming.Producer, digest[:], incoming.ExpiresAt)
	if err != nil || !inbox.Accepted {
		if err == nil {
			err = tx.Commit(ctx)
		}
		return Notification{}, err
	}
	var attemptID, sessionID, verifyMessageID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `SELECT id, session_id, verify_message_id, status FROM activation_attempts WHERE operation_id = $1 FOR UPDATE`, operationID).Scan(&attemptID, &sessionID, &verifyMessageID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrActivationMissing
	}
	if err != nil {
		return Notification{}, err
	}
	if status != "AWAITING_VERIFICATION" {
		if err = message.RejectInbox(ctx, tx, incoming.MessageID); err != nil {
			return Notification{}, err
		}
		return Notification{}, tx.Commit(ctx)
	}
	var botID, userID, chatID int64
	var balanceFingerprint []byte
	var displayLabel *string
	if err = tx.QueryRow(ctx, `SELECT bot_id, telegram_user_id, telegram_chat_id, balance_id_fingerprint, display_label FROM inactive_sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&botID, &userID, &chatID, &balanceFingerprint, &displayLabel); err != nil {
		return Notification{}, err
	}
	text := "Активация не выполнена. Попробуйте начать заново командой /start."
	if activated != nil {
		activatedAt, parseErr := time.Parse(time.RFC3339Nano, activated.ActivatedAt)
		if parseErr != nil || activated.SessionID != sessionID.String() || activated.Status != "ACTIVE" || !hmac.Equal(s.fingerprint(activated.BalanceID), balanceFingerprint) {
			return Notification{}, errors.New("invalid activation result")
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO active_session_projection (session_id, bot_id, telegram_user_id, telegram_chat_id,
				balance_id_fingerprint, display_label, authority_version, activated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (session_id) DO UPDATE SET authority_version = EXCLUDED.authority_version,
				activated_at = EXCLUDED.activated_at, updated_at = now()
			WHERE active_session_projection.authority_version < EXCLUDED.authority_version`,
			sessionID, botID, userID, chatID, balanceFingerprint, displayLabel, activated.AuthorityVersion, activatedAt)
		if err != nil {
			return Notification{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE activation_attempts SET status = 'ACTIVE', completed_at = now(), updated_at = now(), version = version + 1 WHERE id = $1`, attemptID)
		if err != nil {
			return Notification{}, err
		}
		_, err = tx.Exec(ctx, `DELETE FROM inactive_sessions WHERE id = $1`, sessionID)
		if err != nil {
			return Notification{}, err
		}
		text = "Сессия успешно активирована."
	} else {
		_, err = tx.Exec(ctx, `UPDATE activation_attempts SET status = 'REJECTED', failure_code = $2, completed_at = now(), updated_at = now(), version = version + 1 WHERE id = $1`, attemptID, rejection.Code)
		if err != nil {
			return Notification{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE inactive_sessions SET dialog_state = 'REJECTED', updated_at = now(), version = version + 1 WHERE id = $1`, sessionID)
		if err != nil {
			return Notification{}, err
		}
		if rejection.Code == "WALLET_MISMATCH" {
			text = "Для повторной активации нужен кошелёк, использованный при первой активации."
		}
	}
	if err = message.ConfirmOutbox(ctx, tx, verifyMessageID); err != nil {
		return Notification{}, err
	}
	if err = message.AcknowledgeInbox(ctx, tx, incoming.MessageID); err != nil {
		return Notification{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Notification{}, err
	}
	return Notification{Applied: true, ChatID: chatID, Text: text}, nil
}

func (s *FlowStore) insertOutbox(ctx context.Context, tx pgx.Tx, item message.OutboxMessage) error {
	sealed, err := message.SealOutbox(item, s.keyring, "rw-bot")
	if err != nil {
		return err
	}
	return message.InsertOutbox(ctx, tx, sealed)
}

func (s *FlowStore) fingerprint(value string) []byte {
	return crypto.Fingerprint(s.fingerprintKey, []byte(value))
}

func (s *FlowStore) encrypt(table string, rowID uuid.UUID, column, value string) ([]byte, error) {
	sealed, err := s.keyring.Encrypt([]byte(value), []byte("rw-bot\x00"+table+"\x00"+rowID.String()+"\x00"+column))
	if err != nil {
		return nil, err
	}
	return json.Marshal(sealed)
}

func (s *FlowStore) decrypt(table string, rowID uuid.UUID, column string, encoded []byte) (string, error) {
	var sealed crypto.Ciphertext
	if err := json.Unmarshal(encoded, &sealed); err != nil {
		return "", err
	}
	plain, err := s.keyring.Decrypt(sealed, []byte("rw-bot\x00"+table+"\x00"+rowID.String()+"\x00"+column))
	return string(plain), err
}

func cryptoToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
