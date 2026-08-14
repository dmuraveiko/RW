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

	"github.com/dmuraveiko/RW/internal/bot/contract"
	"github.com/dmuraveiko/RW/internal/bot/domain"
)

func TestActivationFixtures(t *testing.T) {
	reserve := fixture[contract.ActivationReserve](t, "activation-reserve-v1.json")
	if err := domain.ValidateWallet(reserve.SenderWallet); err != nil {
		t.Fatal(err)
	}
	reserved := fixture[contract.ActivationReserved](t, "activation-reserved-v1.json")
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := domain.ValidateReserved(reserved, reserve.OperationID, reserve.VerificationAmount, now); err != nil {
		t.Fatal(err)
	}
	payment := fixture[contract.PaymentConfirmed](t, "activation-payment-confirmed-v1.json")
	expected := contract.PaymentConfirmed{OperationID: reserve.OperationID, ExternalReservationID: reserved.ExternalReservationID, SenderWallet: reserve.SenderWallet, ReceiverWallet: reserved.ReceiverWallet, Amount: reserved.Amount}
	if err := domain.ValidatePayment(payment, expected, now.Add(10*time.Minute), now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func fixture[T any](t *testing.T, name string) T {
	t.Helper()
	path := filepath.Join("..", "..", "..", "contracts", "fixtures", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatal("unexpected trailing JSON value")
	}
	return result
}
