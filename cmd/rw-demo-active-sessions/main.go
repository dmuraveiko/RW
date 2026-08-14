package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/dmuraveiko/RW/internal/platform/message"
	"github.com/dmuraveiko/RW/internal/sessions/contract"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

type result struct {
	MessageID uuid.UUID
	Operation uuid.UUID
	Status    string
	Code      string
}

type fakeTopup struct {
	connection *nats.Conn
	verify     message.Codec
	sign       message.Codec
	mu         sync.Mutex
	responses  map[string][]byte
}

func main() {
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	natsURL := env("RW_NATS_URLS", "nats://127.0.0.1:4222")
	trusted, err := crypto.LoadTrustedKeys(required("RW_TRUSTED_KEYS_FILE"))
	if err != nil {
		return err
	}
	botKey, err := crypto.LoadSigningPrivateKey("", required("RW_BOT_SIGNING_PRIVATE_KEY_BASE64"))
	if err != nil {
		return err
	}
	topupKey, err := crypto.LoadSigningPrivateKey("", required("RW_TOPUP_SIGNING_PRIVATE_KEY_BASE64"))
	if err != nil {
		return err
	}
	connection, err := nats.Connect(natsURL, nats.Name("rw-active-sessions-demo"), nats.Timeout(5*time.Second))
	if err != nil {
		return fmt.Errorf("connect to NATS: %w", err)
	}
	defer connection.Close()
	botCodec := message.Codec{Producer: "rw-bot", KeyID: "bot-local-test", PrivateKey: botKey, Trusted: trusted, ClockSkew: 2 * time.Minute}
	topup := &fakeTopup{connection: connection, verify: message.Codec{Trusted: trusted, ClockSkew: 2 * time.Minute}, sign: message.Codec{Producer: "rw-topup", KeyID: "topup-local-test", PrivateKey: topupKey}, responses: make(map[string][]byte)}
	if _, err = connection.Subscribe(contract.TopupVerifySubject, topup.handle); err != nil {
		return err
	}
	results := make(chan result, 16)
	if _, err = connection.Subscribe(contract.ActivationActivated, resultHandler(botCodec, results)); err != nil {
		return err
	}
	if _, err = connection.Subscribe(contract.ActivationRejected, resultHandler(botCodec, results)); err != nil {
		return err
	}
	if err = connection.Flush(); err != nil {
		return err
	}

	runID := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	balanceID := "demo-balance-" + runID
	wallet := "0x1111111111111111111111111111111111111111"
	userBase := time.Now().UTC().UnixMilli()%1_000_000_000 + 1_000_000_000
	first, encoded, err := issue(ctx, connection, botCodec, results, activation(balanceID, wallet, userBase, "demo-tx-1-"+runID))
	if err != nil {
		return err
	}
	if first.Status != "ACTIVE" {
		return fmt.Errorf("first activation failed: %s", first.Code)
	}
	if err = connection.Publish(contract.ActivationVerifySubject, encoded); err != nil {
		return err
	}
	duplicate, err := waitResult(ctx, results, first.Operation, 10*time.Second)
	if err != nil {
		return err
	}
	if duplicate.MessageID != first.MessageID {
		return errors.New("duplicate command produced a different result message")
	}
	second, _, err := issue(ctx, connection, botCodec, results, activation(balanceID, wallet, userBase+1, "demo-tx-2-"+runID))
	if err != nil {
		return err
	}
	if second.Status != "ACTIVE" {
		return fmt.Errorf("repeat activation failed: %s", second.Code)
	}
	mismatch, _, err := issue(ctx, connection, botCodec, results, activation(balanceID, "0x2222222222222222222222222222222222222222", userBase+2, "demo-tx-3-"+runID))
	if err != nil {
		return err
	}
	if mismatch.Code != "WALLET_MISMATCH" {
		return fmt.Errorf("wallet mismatch returned %q", mismatch.Code)
	}
	late, _, err := issue(ctx, connection, botCodec, results, activation(balanceID, wallet, userBase+3, "late-demo-tx-"+runID))
	if err != nil {
		return err
	}
	if late.Code != "CONTRACT_VIOLATION" {
		return fmt.Errorf("late verification returned %q", late.Code)
	}
	summary := map[string]any{
		"first_activation":        "ACTIVE",
		"duplicate_result_reused": true,
		"repeat_activation":       "ACTIVE",
		"different_wallet":        mismatch.Code,
		"late_topup_result":       late.Code,
	}
	encodedSummary, _ := json.MarshalIndent(summary, "", "  ")
	_, _ = os.Stdout.Write(append(encodedSummary, '\n'))
	return nil
}

func activation(balanceID, wallet string, telegramUserID int64, transactionID string) contract.ActivationVerify {
	operationID, _ := uuid.NewV7()
	sessionID, _ := uuid.NewV7()
	return contract.ActivationVerify{
		OperationID: operationID.String(), SessionID: sessionID.String(), BalanceID: balanceID,
		BotID: 100001, TelegramUserID: telegramUserID, TelegramChatID: telegramUserID,
		SenderWallet: wallet, ReceiverWallet: "0x9999999999999999999999999999999999999999",
		Amount: "0.001001", Asset: "USDT", Network: "fixture-net", TransactionID: transactionID,
		ExternalReservationID: "reservation-" + transactionID,
		OfferValidFrom:        contract.Timestamp(time.Now().UTC().Add(-time.Minute)),
		OfferExpiresAt:        contract.Timestamp(time.Now().UTC().Add(30 * time.Minute)), DisplayLabel: "Demo Telegram",
	}
}

func issue(ctx context.Context, connection *nats.Conn, codec message.Codec, results <-chan result, command contract.ActivationVerify) (result, []byte, error) {
	operationID, _ := uuid.Parse(command.OperationID)
	expires := time.Now().UTC().Add(45 * time.Minute)
	item, err := codec.Sign(contract.ActivationVerifySubject, contract.ActivationVerifyType, operationID, uuid.Nil, command, expires)
	if err != nil {
		return result{}, nil, err
	}
	if err = connection.Publish(item.Subject, item.Envelope); err != nil {
		return result{}, nil, err
	}
	value, err := waitResult(ctx, results, operationID, 10*time.Second)
	return value, item.Envelope, err
}

func resultHandler(codec message.Codec, results chan<- result) nats.MsgHandler {
	return func(item *nats.Msg) {
		messageType := contract.ActivationActivatedType
		if item.Subject == contract.ActivationRejected {
			messageType = contract.ActivationRejectedType
		}
		verified, err := codec.Verify(item.Data, item.Subject, messageType, "rw-active-sessions")
		if err != nil {
			return
		}
		if item.Subject == contract.ActivationActivated {
			payload, decodeErr := message.DecodePayload[contract.Activated](verified.Payload)
			if decodeErr == nil {
				operationID, _ := uuid.Parse(payload.OperationID)
				results <- result{MessageID: verified.ID, Operation: operationID, Status: payload.Status}
			}
			return
		}
		payload, decodeErr := message.DecodePayload[contract.Rejection](verified.Payload)
		if decodeErr == nil {
			operationID, _ := uuid.Parse(payload.OperationID)
			results <- result{MessageID: verified.ID, Operation: operationID, Status: "REJECTED", Code: payload.Code}
		}
	}
}

func (f *fakeTopup) handle(item *nats.Msg) {
	verified, err := f.verify.Verify(item.Data, item.Subject, contract.TopupVerifyType, "rw-active-sessions")
	if err != nil {
		return
	}
	payload, err := message.DecodePayload[contract.TopupVerify](verified.Payload)
	if err != nil {
		return
	}
	f.mu.Lock()
	response := f.responses[payload.OperationID]
	if response == nil {
		operationID, parseErr := uuid.Parse(payload.OperationID)
		if parseErr == nil {
			finalizedAt := time.Now().UTC()
			if strings.HasPrefix(payload.TransactionID, "late-") {
				finalizedAt = finalizedAt.Add(time.Hour)
			}
			resultPayload := contract.TopupVerified{OperationID: payload.OperationID, TransactionID: payload.TransactionID, ExternalReservationID: payload.ExternalReservationID, SenderWallet: payload.SenderWallet, ReceiverWallet: payload.ReceiverWallet, Amount: payload.Amount, Asset: payload.Asset, Network: payload.Network, FinalizedAt: contract.Timestamp(finalizedAt)}
			signed, signErr := f.sign.Sign(contract.TopupVerifiedSubject, contract.TopupVerifiedType, operationID, verified.ID, resultPayload, time.Now().UTC().Add(10*time.Minute))
			if signErr == nil {
				response = signed.Envelope
				f.responses[payload.OperationID] = response
			}
		}
	}
	f.mu.Unlock()
	if response != nil {
		_ = f.connection.Publish(contract.TopupVerifiedSubject, response)
	}
}

func waitResult(ctx context.Context, results <-chan result, operationID uuid.UUID, timeout time.Duration) (result, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return result{}, ctx.Err()
		case <-timer.C:
			return result{}, errors.New("timed out waiting for activation result")
		case value := <-results:
			if value.Operation == operationID {
				return value, nil
			}
		}
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
