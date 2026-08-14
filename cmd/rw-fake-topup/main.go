package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	botcontract "github.com/dmuraveiko/RW/internal/bot/contract"
	"github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/dmuraveiko/RW/internal/platform/message"
	sessionscontract "github.com/dmuraveiko/RW/internal/sessions/contract"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

type service struct {
	connection *nats.Conn
	codec      message.Codec
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	trusted, err := crypto.LoadTrustedKeys(required("RW_TRUSTED_KEYS_FILE"))
	if err != nil {
		return err
	}
	privateKey, err := crypto.LoadSigningPrivateKey("", required("RW_TOPUP_SIGNING_PRIVATE_KEY_BASE64"))
	if err != nil {
		return err
	}
	connection, err := nats.Connect(env("RW_NATS_URLS", "nats://127.0.0.1:4222"), nats.Name("rw-fake-topup"), nats.Timeout(5*time.Second))
	if err != nil {
		return err
	}
	defer connection.Close()
	instance := &service{connection: connection, codec: message.Codec{Producer: "rw-topup", KeyID: "topup-local-test", PrivateKey: privateKey, Trusted: trusted, ClockSkew: 2 * time.Minute}}
	reserve, err := connection.QueueSubscribe(botcontract.ActivationReserveSubject, "rw.topup.local", instance.handleReserve)
	if err != nil {
		return err
	}
	verify, err := connection.QueueSubscribe(sessionscontract.TopupVerifySubject, "rw.topup.local", instance.handleVerify)
	if err != nil {
		return err
	}
	if err = connection.Flush(); err != nil {
		return err
	}
	<-ctx.Done()
	_ = reserve.Unsubscribe()
	_ = verify.Unsubscribe()
	return nil
}

func (s *service) handleReserve(item *nats.Msg) {
	verified, err := s.codec.Verify(item.Data, item.Subject, botcontract.ActivationReserveType, "rw-bot")
	if err != nil {
		return
	}
	request, err := message.DecodePayload[botcontract.ActivationReserve](verified.Payload)
	if err != nil {
		return
	}
	operationID, err := uuid.Parse(request.OperationID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	reservationID := "local-reservation-" + operationID.String()
	reserved := botcontract.ActivationReserved{OperationID: request.OperationID, ExternalReservationID: reservationID, ReceiverWallet: "0x9999999999999999999999999999999999999999", Amount: request.VerificationAmount, ValidFrom: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339Nano)}
	signed, err := s.codec.Sign(botcontract.ActivationReservedSubject, botcontract.ActivationReservedType, operationID, verified.ID, reserved, now.Add(15*time.Minute))
	if err != nil || s.connection.Publish(signed.Subject, signed.Envelope) != nil {
		return
	}
	timer := time.NewTimer(2 * time.Second)
	go func() {
		defer timer.Stop()
		<-timer.C
		observed := time.Now().UTC()
		payment := botcontract.PaymentConfirmed{OperationID: request.OperationID, ExternalReservationID: reservationID, TransactionID: "local-transaction-" + operationID.String(), SenderWallet: request.SenderWallet, ReceiverWallet: reserved.ReceiverWallet, Amount: request.VerificationAmount, ObservedAt: observed.Format(time.RFC3339Nano)}
		result, signErr := s.codec.Sign(botcontract.PaymentConfirmedSubject, botcontract.PaymentConfirmedType, operationID, signed.MessageID, payment, observed.Add(15*time.Minute))
		if signErr == nil {
			_ = s.connection.Publish(result.Subject, result.Envelope)
		}
	}()
}

func (s *service) handleVerify(item *nats.Msg) {
	verified, err := s.codec.Verify(item.Data, item.Subject, sessionscontract.TopupVerifyType, "rw-active-sessions")
	if err != nil {
		return
	}
	request, err := message.DecodePayload[sessionscontract.TopupVerify](verified.Payload)
	if err != nil {
		return
	}
	operationID, err := uuid.Parse(request.OperationID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	result := sessionscontract.TopupVerified{OperationID: request.OperationID, TransactionID: request.TransactionID, ExternalReservationID: request.ExternalReservationID, SenderWallet: request.SenderWallet, ReceiverWallet: request.ReceiverWallet, Amount: request.Amount, Asset: request.Asset, Network: request.Network, FinalizedAt: now.Format(time.RFC3339Nano)}
	signed, err := s.codec.Sign(sessionscontract.TopupVerifiedSubject, sessionscontract.TopupVerifiedType, operationID, verified.ID, result, now.Add(10*time.Minute))
	if err == nil {
		_ = s.connection.Publish(signed.Subject, signed.Envelope)
	}
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		panic(name + " is required")
	}
	return value
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
