package app

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	botpostgres "github.com/dmuraveiko/RW/internal/bot/adapter/postgres"
	"github.com/dmuraveiko/RW/internal/bot/contract"
	"github.com/dmuraveiko/RW/internal/bot/domain"
	"github.com/dmuraveiko/RW/internal/platform/config"
	"github.com/dmuraveiko/RW/internal/platform/message"
	sessionscontract "github.com/dmuraveiko/RW/internal/sessions/contract"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

type ActivationService struct {
	store      *botpostgres.FlowStore
	codec      message.Codec
	config     config.Config
	logger     *slog.Logger
	connection *nats.Conn
	messenger  Messenger
	onReady    func()
	identityMu sync.RWMutex
	botID      int64
	username   string
}

func NewActivationService(store *botpostgres.FlowStore, codec message.Codec, cfg config.Config, logger *slog.Logger, connection *nats.Conn, messenger Messenger, onReady func()) *ActivationService {
	return &ActivationService{store: store, codec: codec, config: cfg, logger: logger, connection: connection, messenger: messenger, onReady: onReady, botID: cfg.Bot.TelegramBotID, username: strings.TrimPrefix(cfg.Bot.TelegramBotUsername, "@")}
}

func (s *ActivationService) SetTelegramIdentity(botID int64, username string) {
	s.identityMu.Lock()
	s.botID = botID
	s.username = strings.TrimPrefix(username, "@")
	s.identityMu.Unlock()
}

func (s *ActivationService) Run(ctx context.Context) error {
	type subscription struct {
		subject string
		handler message.Handler
	}
	subscriptions := []subscription{
		{contract.InviteCreateSubject, s.handleInviteCreate},
		{contract.ActivationReservedSubject, s.handleReserved},
		{contract.ReserveRejectedSubject, s.handleReserveRejected},
		{contract.PaymentConfirmedSubject, s.handlePaymentConfirmed},
		{sessionscontract.ActivationActivated, s.handleActivated},
		{sessionscontract.ActivationRejected, s.handleActivationRejected},
	}
	consumers := make([]*message.Consumer, 0, len(subscriptions))
	for _, item := range subscriptions {
		consumer, err := message.StartConsumer(ctx, s.connection, item.subject, "rw.bot.v1", s.config.NATS, s.config.Messaging.OutboxConcurrency, s.logged(item.handler))
		if err != nil {
			for _, started := range consumers {
				started.Stop()
			}
			return err
		}
		consumers = append(consumers, consumer)
	}
	publisher := message.NewPublisher(s.store.Pool(), s.connection, s.config.Messaging, s.logger, s.store.Keyring(), "rw-bot")
	if s.onReady != nil {
		s.onReady()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	workerErrors := make(chan error, 2)
	go func() { workerErrors <- publisher.Run(workerCtx) }()
	go func() { workerErrors <- s.reconcile(workerCtx) }()
	var err error
	select {
	case <-ctx.Done():
	case err = <-workerErrors:
	}
	cancel()
	for _, consumer := range consumers {
		consumer.Stop()
	}
	return err
}

func (s *ActivationService) Handle(ctx context.Context, incoming IncomingMessage) (string, error) {
	if incoming.BotID <= 0 || incoming.UserID <= 0 || incoming.ChatID <= 0 {
		return "Не удалось определить Telegram-сессию. Попробуйте ещё раз.", nil
	}
	command, argument := parseCommand(incoming.Text)
	switch command {
	case "start":
		if argument != "" {
			return s.consumeInvite(ctx, incoming, argument)
		}
		return s.statusText(ctx, incoming, true)
	case "status":
		return s.statusText(ctx, incoming, false)
	case "help":
		return "Команды:\n/start <инвайт> — привязать сессию\n/status — проверить активацию\n/help — показать справку", nil
	case "":
		return s.handleText(ctx, incoming)
	default:
		return "Неизвестная команда. Используйте /help.", nil
	}
}

func (s *ActivationService) consumeInvite(ctx context.Context, incoming IncomingMessage, token string) (string, error) {
	if len(token) != 43 {
		return "Инвайт недействителен или срок его действия истёк.", nil
	}
	_, err := s.store.ConsumeInvite(ctx, incoming.BotID, incoming.UserID, incoming.ChatID, trimLabel(incoming.DisplayLabel), token)
	if errors.Is(err, botpostgres.ErrInviteInvalid) {
		return "Инвайт недействителен или срок его действия истёк.", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Сессия привязана.\n\nДля активации отправьте адрес вашего кошелька USDT в сети %s. Перевод при проверке нужно будет выполнить именно с него.", s.config.Bot.USDTNetwork), nil
}

func (s *ActivationService) handleText(ctx context.Context, incoming IncomingMessage) (string, error) {
	state, err := s.store.State(ctx, incoming.BotID, incoming.UserID)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(incoming.Text)
	if !state.Bound {
		return s.consumeInvite(ctx, incoming, text)
	}
	if state.Active {
		return "Сессия активна.", nil
	}
	if state.DialogState != "AWAITING_WALLET" && state.DialogState != "REJECTED" {
		return s.statusForState(state.DialogState), nil
	}
	if err = domain.ValidateWallet(text); err != nil {
		return fmt.Sprintf("Некорректный адрес. Отправьте адрес кошелька USDT в сети %s без пробелов.", s.config.Bot.USDTNetwork), nil
	}
	minor, err := randomMinor(s.config.Bot.ActivationAmountMin, s.config.Bot.ActivationAmountMax)
	if err != nil {
		return "", err
	}
	amount := domain.FormatAmount(minor, s.config.Bot.USDTScale)
	err = s.store.BeginActivation(ctx, incoming.BotID, incoming.UserID, text, amount, s.config.Bot.USDTNetwork, func(draft botpostgres.ActivationDraft) (message.OutboxMessage, error) {
		payload := contract.ActivationReserve{OperationID: draft.OperationID.String(), BalanceID: draft.BalanceID, SessionID: draft.SessionID.String(), SenderWallet: draft.SenderWallet, VerificationAmount: draft.Amount, Asset: "USDT", Network: s.config.Bot.USDTNetwork}
		return s.codec.Sign(contract.ActivationReserveSubject, contract.ActivationReserveType, draft.OperationID, uuid.Nil, payload, time.Now().UTC().Add(2*time.Minute))
	})
	if errors.Is(err, botpostgres.ErrUnexpectedState) {
		return s.statusText(ctx, incoming, false)
	}
	if err != nil {
		return "", err
	}
	return "Кошелёк принят. Запрашиваю реквизиты для проверки активации.", nil
}

func (s *ActivationService) statusText(ctx context.Context, incoming IncomingMessage, start bool) (string, error) {
	state, err := s.store.State(ctx, incoming.BotID, incoming.UserID)
	if err != nil {
		return "", err
	}
	if !state.Bound {
		if start {
			return "Для начала работы отправьте инвайт или откройте ссылку вида https://t.me/<bot>?start=<invite>.", nil
		}
		return "Сессия ещё не привязана. Нужен действующий инвайт.", nil
	}
	if state.Active {
		return "Сессия активна.", nil
	}
	return s.statusForState(state.DialogState), nil
}

func (s *ActivationService) statusForState(state string) string {
	switch state {
	case "AWAITING_WALLET", "REJECTED":
		return fmt.Sprintf("Сессия привязана, но не активирована. Отправьте адрес кошелька USDT в сети %s.", s.config.Bot.USDTNetwork)
	case "AWAITING_RESERVATION":
		return "Запрашиваются реквизиты для активации."
	case "AWAITING_PAYMENT":
		return "Ожидается платёж для активации. Используйте ранее выданные реквизиты."
	case "AWAITING_VERIFICATION":
		return "Платёж найден, выполняется окончательная проверка."
	default:
		return "Состояние сессии уточняется. Попробуйте /status позже."
	}
}

func (s *ActivationService) handleInviteCreate(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, contract.InviteCreateType, "rw-invite-issuer")
	if err != nil {
		return err
	}
	command, err := message.DecodePayload[contract.InviteCreate](verified.Payload)
	if err != nil || domain.ValidateInviteCreate(command) != nil {
		return domain.ErrInvalidInput
	}
	ttl := s.config.Bot.InviteTTL
	if command.RequestedTTLSecond != 0 {
		ttl = time.Duration(command.RequestedTTLSecond) * time.Second
	}
	operationID, _ := uuid.Parse(command.OperationID)
	return s.store.CreateInvite(ctx, incoming(verified, subject), command, ttl, func(token string, expiresAt time.Time) (message.OutboxMessage, error) {
		_, username := s.identity()
		if username == "" {
			return message.OutboxMessage{}, errors.New("telegram bot username is not available")
		}
		payload := contract.InviteCreated{OperationID: command.OperationID, Invite: token, BotDeepLink: "https://t.me/" + username + "?start=" + token, ExpiresAt: expiresAt.Format(time.RFC3339Nano)}
		return s.codec.Sign(contract.InviteCreatedSubject, contract.InviteCreatedType, operationID, verified.ID, payload, time.Now().UTC().Add(5*time.Minute))
	})
}

func (s *ActivationService) handleReserved(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, contract.ActivationReservedType, "rw-topup")
	if err != nil {
		return err
	}
	result, err := message.DecodePayload[contract.ActivationReserved](verified.Payload)
	if err != nil {
		return err
	}
	notification, err := s.store.ApplyReserved(ctx, incoming(verified, subject), result, s.config.Bot.USDTNetwork)
	return s.notify(ctx, notification, err)
}

func (s *ActivationService) handleReserveRejected(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, contract.ReserveRejectedType, "rw-topup")
	if err != nil {
		return err
	}
	result, err := message.DecodePayload[contract.Rejection](verified.Payload)
	if err != nil {
		return err
	}
	notification, err := s.store.ApplyReserveRejected(ctx, incoming(verified, subject), result)
	return s.notify(ctx, notification, err)
}

func (s *ActivationService) reconcile(ctx context.Context) error {
	ticker := time.NewTicker(s.config.Messaging.ReconcileInterval)
	defer ticker.Stop()
	for {
		notifications, err := s.store.ExpireAttempts(ctx, s.config.Messaging.ReconcileBatch)
		if err != nil && ctx.Err() == nil {
			s.logger.Error("bot activation reconciliation failed", "error", err)
		}
		for _, notification := range notifications {
			_ = s.notify(ctx, notification, nil)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *ActivationService) handlePaymentConfirmed(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, contract.PaymentConfirmedType, "rw-topup")
	if err != nil {
		return err
	}
	result, err := message.DecodePayload[contract.PaymentConfirmed](verified.Payload)
	if err != nil {
		return err
	}
	notification, err := s.store.ApplyPayment(ctx, incoming(verified, subject), result, func(draft botpostgres.VerificationDraft, causationID uuid.UUID) (message.OutboxMessage, error) {
		payload := sessionscontract.ActivationVerify{OperationID: draft.OperationID.String(), SessionID: draft.SessionID.String(), BalanceID: draft.BalanceID, BotID: draft.BotID, TelegramUserID: draft.TelegramUserID, TelegramChatID: draft.TelegramChatID, SenderWallet: draft.SenderWallet, ReceiverWallet: draft.ReceiverWallet, Amount: draft.Amount, Asset: "USDT", Network: s.config.Bot.USDTNetwork, TransactionID: draft.TransactionID, ExternalReservationID: draft.ExternalReservationID, OfferValidFrom: draft.OfferValidFrom.Format(time.RFC3339Nano), OfferExpiresAt: draft.OfferExpiresAt.Format(time.RFC3339Nano), DisplayLabel: draft.DisplayLabel}
		expiresAt := draft.OfferExpiresAt.Add(30 * time.Minute)
		return s.codec.Sign(sessionscontract.ActivationVerifySubject, sessionscontract.ActivationVerifyType, draft.OperationID, causationID, payload, expiresAt)
	})
	return s.notify(ctx, notification, err)
}

func (s *ActivationService) handleActivated(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, sessionscontract.ActivationActivatedType, "rw-active-sessions")
	if err != nil {
		return err
	}
	result, err := message.DecodePayload[sessionscontract.Activated](verified.Payload)
	if err != nil {
		return err
	}
	notification, err := s.store.CompleteActivation(ctx, incoming(verified, subject), &result, nil)
	return s.notify(ctx, notification, err)
}

func (s *ActivationService) handleActivationRejected(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, sessionscontract.ActivationRejectedType, "rw-active-sessions")
	if err != nil {
		return err
	}
	result, err := message.DecodePayload[sessionscontract.Rejection](verified.Payload)
	if err != nil {
		return err
	}
	notification, err := s.store.CompleteActivation(ctx, incoming(verified, subject), nil, &result)
	return s.notify(ctx, notification, err)
}

func (s *ActivationService) notify(ctx context.Context, notification botpostgres.Notification, err error) error {
	if err != nil || !notification.Applied || s.messenger == nil {
		return err
	}
	if sendErr := s.messenger.SendText(ctx, notification.ChatID, notification.Text); sendErr != nil {
		s.logger.Warn("Telegram activation notification outcome is unknown", "chat_id", notification.ChatID)
	}
	return nil
}

func (s *ActivationService) logged(handler message.Handler) message.Handler {
	return func(ctx context.Context, subject string, data []byte) error {
		err := handler(ctx, subject, data)
		if err != nil && ctx.Err() == nil {
			s.logger.Error("message processing failed", "subject", subject, "error", err)
		}
		return err
	}
}

func (s *ActivationService) identity() (int64, string) {
	s.identityMu.RLock()
	defer s.identityMu.RUnlock()
	return s.botID, s.username
}

func incoming(verified message.Verified, subject string) botpostgres.IncomingMessage {
	return botpostgres.IncomingMessage{MessageID: verified.ID, Subject: subject, Producer: verified.Envelope.Producer, Payload: verified.Payload, ExpiresAt: verified.Expires}
}

func randomMinor(minimum, maximum int64) (int64, error) {
	if minimum > maximum {
		return 0, errors.New("invalid activation amount range")
	}
	span := uint64(maximum-minimum) + 1
	limit := ^uint64(0) - (^uint64(0) % span)
	var buffer [8]byte
	for {
		if _, err := rand.Read(buffer[:]); err != nil {
			return 0, err
		}
		value := binary.BigEndian.Uint64(buffer[:])
		if value < limit {
			return minimum + int64(value%span), nil
		}
	}
}

func trimLabel(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}
