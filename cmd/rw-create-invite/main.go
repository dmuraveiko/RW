package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dmuraveiko/RW/internal/bot/contract"
	"github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/dmuraveiko/RW/internal/platform/message"
	"github.com/google/uuid"
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
	privateKey, err := crypto.LoadSigningPrivateKey("", required("RW_ISSUER_SIGNING_PRIVATE_KEY_BASE64"))
	if err != nil {
		return err
	}
	connection, err := nats.Connect(env("RW_NATS_URLS", "nats://127.0.0.1:4222"), nats.Name("rw-create-invite"), nats.Timeout(5*time.Second))
	if err != nil {
		return err
	}
	defer connection.Close()
	codec := message.Codec{Producer: "rw-invite-issuer", KeyID: "issuer-local-test", PrivateKey: privateKey, Trusted: trusted, ClockSkew: 2 * time.Minute}
	operationID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	results := make(chan contract.InviteCreated, 1)
	subscription, err := connection.Subscribe(contract.InviteCreatedSubject, func(item *nats.Msg) {
		verified, verifyErr := codec.Verify(item.Data, item.Subject, contract.InviteCreatedType, "rw-bot")
		if verifyErr != nil {
			return
		}
		payload, decodeErr := message.DecodePayload[contract.InviteCreated](verified.Payload)
		if decodeErr == nil && payload.OperationID == operationID.String() {
			select {
			case results <- payload:
			default:
			}
		}
	})
	if err != nil {
		return err
	}
	defer subscription.Unsubscribe()
	if err = connection.Flush(); err != nil {
		return err
	}
	payload := contract.InviteCreate{OperationID: operationID.String(), BalanceID: required("RW_BALANCE_ID")}
	command, err := codec.Sign(contract.InviteCreateSubject, contract.InviteCreateType, operationID, uuid.Nil, payload, time.Now().UTC().Add(5*time.Minute))
	if err != nil {
		return err
	}
	if err = connection.Publish(command.Subject, command.Envelope); err != nil {
		return err
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("timed out waiting for invite")
	case result := <-results:
		encoded, _ := json.MarshalIndent(result, "", "  ")
		_, err = os.Stdout.Write(append(encoded, '\n'))
		return err
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
