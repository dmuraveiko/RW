package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	botpostgres "github.com/dmuraveiko/RW/internal/bot/adapter/postgres"
	botapp "github.com/dmuraveiko/RW/internal/bot/app"
	"github.com/dmuraveiko/RW/internal/bot/contract"
	"github.com/dmuraveiko/RW/internal/platform/config"
	"github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/dmuraveiko/RW/internal/platform/message"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

func main() {
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	trusted, err := crypto.LoadTrustedKeys(required("RW_TRUSTED_KEYS_FILE"))
	if err != nil {
		return err
	}
	keyring, err := crypto.LoadDataKeyring(required("RW_DATA_KEYRING_FILE"))
	if err != nil {
		return err
	}
	fingerprintKey, err := crypto.LoadFingerprintKey(required("RW_FINGERPRINT_KEY_FILE"))
	if err != nil {
		return err
	}
	botKey, err := crypto.LoadSigningPrivateKey("", required("RW_BOT_SIGNING_PRIVATE_KEY_BASE64"))
	if err != nil {
		return err
	}
	issuerKey, err := crypto.LoadSigningPrivateKey("", required("RW_ISSUER_SIGNING_PRIVATE_KEY_BASE64"))
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, required("RW_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	connection, err := nats.Connect(env("RW_NATS_URLS", "nats://127.0.0.1:4222"), nats.Name("rw-telegram-flow-demo"), nats.Timeout(5*time.Second))
	if err != nil {
		return err
	}
	defer connection.Close()
	issuerCodec := message.Codec{Producer: "rw-invite-issuer", KeyID: "issuer-local-test", PrivateKey: issuerKey, Trusted: trusted, ClockSkew: 2 * time.Minute}
	botCodec := message.Codec{Producer: "rw-bot", KeyID: "bot-local-test", PrivateKey: botKey, Trusted: trusted, ClockSkew: 2 * time.Minute}
	invite, err := createInvite(ctx, connection, issuerCodec)
	if err != nil {
		return err
	}
	cfg := config.Config{Bot: config.BotConfig{TelegramBotID: 100001, TelegramBotUsername: "realwallet_local_bot", InviteTTL: 24 * time.Hour, USDTNetwork: "fixture-net", USDTScale: 6, ActivationAmountMin: 1000, ActivationAmountMax: 9999}}
	store := botpostgres.NewFlowStore(pool, keyring, fingerprintKey)
	service := botapp.NewActivationService(store, botCodec, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), connection, nil, nil)
	userID := time.Now().UTC().UnixNano()%1_000_000_000 + 2_000_000_000
	bound, err := service.Handle(ctx, botapp.IncomingMessage{BotID: 100001, UserID: userID, ChatID: userID, ChatType: "private", Text: "/start " + invite.Invite, DisplayLabel: "Telegram Demo"})
	if err != nil {
		return err
	}
	walletAccepted, err := service.Handle(ctx, botapp.IncomingMessage{BotID: 100001, UserID: userID, ChatID: userID, ChatType: "private", Text: "0x1111111111111111111111111111111111111111", DisplayLabel: "Telegram Demo"})
	if err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := store.State(ctx, 100001, userID)
		if stateErr != nil {
			return stateErr
		}
		if state.Active {
			result := map[string]any{"invite_created": invite.Invite != "", "binding_response": bound, "wallet_response": walletAccepted, "final_status": "ACTIVE"}
			encoded, _ := json.MarshalIndent(result, "", "  ")
			_, err = os.Stdout.Write(append(encoded, '\n'))
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("timed out waiting for Telegram activation flow")
}

func createInvite(ctx context.Context, connection *nats.Conn, codec message.Codec) (contract.InviteCreated, error) {
	operationID, err := uuid.NewV7()
	if err != nil {
		return contract.InviteCreated{}, err
	}
	results := make(chan contract.InviteCreated, 1)
	subscription, err := connection.Subscribe(contract.InviteCreatedSubject, func(item *nats.Msg) {
		verified, verifyErr := codec.Verify(item.Data, item.Subject, contract.InviteCreatedType, "rw-bot")
		if verifyErr != nil {
			return
		}
		payload, decodeErr := message.DecodePayload[contract.InviteCreated](verified.Payload)
		if decodeErr == nil && payload.OperationID == operationID.String() {
			results <- payload
		}
	})
	if err != nil {
		return contract.InviteCreated{}, err
	}
	defer subscription.Unsubscribe()
	if err = connection.Flush(); err != nil {
		return contract.InviteCreated{}, err
	}
	payload := contract.InviteCreate{OperationID: operationID.String(), BalanceID: "telegram-demo-balance-" + operationID.String()}
	command, err := codec.Sign(contract.InviteCreateSubject, contract.InviteCreateType, operationID, uuid.Nil, payload, time.Now().UTC().Add(5*time.Minute))
	if err != nil {
		return contract.InviteCreated{}, err
	}
	if err = connection.Publish(command.Subject, command.Envelope); err != nil {
		return contract.InviteCreated{}, err
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return contract.InviteCreated{}, ctx.Err()
	case <-timer.C:
		return contract.InviteCreated{}, errors.New("timed out waiting for invite")
	case result := <-results:
		return result, nil
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
