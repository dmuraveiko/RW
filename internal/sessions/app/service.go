package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dmuraveiko/RW/internal/platform/config"
	"github.com/dmuraveiko/RW/internal/platform/message"
	sessionspostgres "github.com/dmuraveiko/RW/internal/sessions/adapter/postgres"
	"github.com/dmuraveiko/RW/internal/sessions/contract"
	"github.com/dmuraveiko/RW/internal/sessions/domain"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

type Service struct {
	store      *sessionspostgres.Store
	codec      message.Codec
	config     config.Config
	logger     *slog.Logger
	connection *nats.Conn
	onReady    func()
}

func NewService(store *sessionspostgres.Store, codec message.Codec, cfg config.Config, logger *slog.Logger, connection *nats.Conn, onReady func()) *Service {
	return &Service{store: store, codec: codec, config: cfg, logger: logger, connection: connection, onReady: onReady}
}

func (s *Service) Run(ctx context.Context) error {
	wrap := func(handler message.Handler) message.Handler {
		return func(ctx context.Context, subject string, data []byte) error {
			err := handler(ctx, subject, data)
			if err != nil && ctx.Err() == nil {
				s.logger.Error("message processing failed", "subject", subject, "error", err)
			}
			return err
		}
	}
	activation, err := message.StartConsumer(ctx, s.connection, contract.ActivationVerifySubject, "rw.sessions.v1", s.config.NATS, s.config.Messaging.OutboxConcurrency, wrap(s.handleActivationVerify))
	if err != nil {
		return err
	}
	verified, err := message.StartConsumer(ctx, s.connection, contract.TopupVerifiedSubject, "rw.sessions.v1", s.config.NATS, s.config.Messaging.OutboxConcurrency, wrap(s.handleTopupVerified))
	if err != nil {
		activation.Stop()
		return err
	}
	rejected, err := message.StartConsumer(ctx, s.connection, contract.TopupRejectedSubject, "rw.sessions.v1", s.config.NATS, s.config.Messaging.OutboxConcurrency, wrap(s.handleTopupRejected))
	if err != nil {
		activation.Stop()
		verified.Stop()
		return err
	}
	publisher := message.NewPublisher(s.store.Pool(), s.connection, s.config.Messaging, s.logger, s.store.Keyring(), "rw-active-sessions")
	if s.onReady != nil {
		s.onReady()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerErrors := make(chan error, 2)
	go func() { workerErrors <- publisher.Run(workerCtx) }()
	go func() { workerErrors <- s.reconcile(workerCtx) }()
	select {
	case <-ctx.Done():
		err = nil
	case err = <-workerErrors:
		cancel()
	}
	activation.Stop()
	verified.Stop()
	rejected.Stop()
	return err
}

func (s *Service) reconcile(ctx context.Context) error {
	ticker := time.NewTicker(s.config.Messaging.ReconcileInterval)
	defer ticker.Stop()
	for {
		if err := s.reconcileOnce(ctx); err != nil && ctx.Err() == nil {
			s.logger.Error("activation reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Service) reconcileOnce(ctx context.Context) error {
	items, err := s.store.PendingRetries(ctx, s.config.Messaging.ReconcileBatch)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, item := range items {
		if !item.OfferExpiresAt.After(now) {
			if err = s.store.ExpireVerification(ctx, item, s.resultFactory(item.TopupCommandMessageID)); err != nil {
				return err
			}
			continue
		}
		expires := now.Add(5 * time.Minute)
		if item.OfferExpiresAt.Before(expires) {
			expires = item.OfferExpiresAt
		}
		replacement, signErr := s.codec.Sign(contract.TopupVerifySubject, contract.TopupVerifyType, item.OperationID, item.TopupCommandMessageID, item.Facts, expires)
		if signErr != nil {
			return signErr
		}
		replacement.Kind = "COMMAND"
		if err = s.store.ReplaceTopupCommand(ctx, item, replacement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleActivationVerify(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, contract.ActivationVerifyType, "rw-bot")
	if err != nil {
		return fmt.Errorf("verify activation command: %w", err)
	}
	command, err := message.DecodePayload[contract.ActivationVerify](verified.Payload)
	if err != nil {
		return err
	}
	if err = domain.ValidateActivation(command, s.config.Sessions.USDTNetwork, time.Now().UTC()); err != nil {
		return err
	}
	operationID, _ := uuid.Parse(command.OperationID)
	topupPayload := contract.TopupVerify{
		OperationID: command.OperationID, TransactionID: command.TransactionID,
		ExternalReservationID: command.ExternalReservationID, SenderWallet: command.SenderWallet,
		ReceiverWallet: command.ReceiverWallet, Amount: command.Amount, Asset: command.Asset, Network: command.Network,
	}
	topupExpiry := time.Now().UTC().Add(5 * time.Minute)
	if verified.Expires.Before(topupExpiry) {
		topupExpiry = verified.Expires
	}
	topup, err := s.codec.Sign(contract.TopupVerifySubject, contract.TopupVerifyType, operationID, verified.ID, topupPayload, topupExpiry)
	if err != nil {
		return err
	}
	topup.Kind = "COMMAND"
	return s.store.AcceptVerification(ctx, sessionspostgres.Incoming{MessageID: verified.ID, Subject: subject, Producer: verified.Envelope.Producer, Payload: verified.Payload, ExpiresAt: verified.Expires}, command, topup)
}

func (s *Service) handleTopupVerified(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, contract.TopupVerifiedType, "rw-topup")
	if err != nil {
		return fmt.Errorf("verify top-up result: %w", err)
	}
	payload, err := message.DecodePayload[contract.TopupVerified](verified.Payload)
	if err != nil {
		return err
	}
	return s.store.ApplyTopupVerified(ctx, sessionspostgres.Incoming{MessageID: verified.ID, Subject: subject, Producer: verified.Envelope.Producer, Payload: verified.Payload, ExpiresAt: verified.Expires}, payload, s.resultFactory(verified.ID))
}

func (s *Service) handleTopupRejected(ctx context.Context, subject string, data []byte) error {
	verified, err := s.codec.Verify(data, subject, contract.TopupRejectedType, "rw-topup")
	if err != nil {
		return fmt.Errorf("verify top-up rejection: %w", err)
	}
	payload, err := message.DecodePayload[contract.Rejection](verified.Payload)
	if err != nil {
		return err
	}
	if payload.OperationID == "" || payload.Code == "" {
		return errors.New("invalid top-up rejection")
	}
	return s.store.ApplyTopupRejected(ctx, sessionspostgres.Incoming{MessageID: verified.ID, Subject: subject, Producer: verified.Envelope.Producer, Payload: verified.Payload, ExpiresAt: verified.Expires}, payload, s.resultFactory(verified.ID))
}

func (s *Service) resultFactory(causationID uuid.UUID) sessionspostgres.ResultFactory {
	return func(completion sessionspostgres.Completion) (message.OutboxMessage, error) {
		expires := completion.OccurredAt.Add(24 * time.Hour)
		if completion.Activated {
			payload := contract.Activated{OperationID: completion.OperationID.String(), SessionID: completion.SessionID.String(), BalanceID: completion.BalanceID, Status: "ACTIVE", AuthorityVersion: completion.AuthorityVersion, ActivatedAt: contract.Timestamp(completion.OccurredAt)}
			return s.codec.Sign(contract.ActivationActivated, contract.ActivationActivatedType, completion.OperationID, causationID, payload, expires)
		}
		payload := contract.Rejection{OperationID: completion.OperationID.String(), Code: completion.Code, Retryable: completion.Retryable}
		return s.codec.Sign(contract.ActivationRejected, contract.ActivationRejectedType, completion.OperationID, causationID, payload, expires)
	}
}
