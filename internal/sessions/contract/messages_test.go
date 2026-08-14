package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmuraveiko/RW/internal/sessions/contract"
	"github.com/dmuraveiko/RW/internal/sessions/domain"
)

func TestActivationVerifyFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "contracts", "fixtures", "sessions-activation-verify-v1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decodeStrict[contract.ActivationVerify](data)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if err = domain.ValidateActivation(payload, "fixture-net", now); err != nil {
		t.Fatal(err)
	}
}

func TestTopupVerifiedFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "contracts", "fixtures", "topup-activation-verified-v1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decodeStrict[contract.TopupVerified](data)
	if err != nil {
		t.Fatal(err)
	}
	expected := contract.TopupVerify{OperationID: payload.OperationID, TransactionID: payload.TransactionID, ExternalReservationID: payload.ExternalReservationID, SenderWallet: payload.SenderWallet, ReceiverWallet: payload.ReceiverWallet, Amount: payload.Amount, Asset: payload.Asset, Network: payload.Network}
	if err = domain.ValidateTopupResult(payload, expected); err != nil {
		t.Fatal(err)
	}
}

func decodeStrict[T any](data []byte) (T, error) {
	var result T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return result, errors.New("unexpected trailing JSON value")
	}
	return result, nil
}
