package domain

import (
	"testing"
	"time"

	"github.com/dmuraveiko/RW/internal/sessions/contract"
)

func TestValidateActivation(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	valid := contract.ActivationVerify{
		OperationID: "0198a123-4567-7abc-8def-0123456789ab",
		SessionID:   "0198a123-4567-7abc-8def-1123456789ab",
		BalanceID:   "balance-001", BotID: 1, TelegramUserID: 2, TelegramChatID: 2,
		SenderWallet:   "0x1111111111111111111111111111111111111111",
		ReceiverWallet: "0x9999999999999999999999999999999999999999",
		Amount:         "0.001001", Asset: "USDT", Network: "fixture-net", TransactionID: "tx-001",
		ExternalReservationID: "reservation-001", OfferValidFrom: contract.Timestamp(now.Add(-time.Minute)), OfferExpiresAt: contract.Timestamp(now.Add(30 * time.Minute)),
	}
	if err := ValidateActivation(valid, "fixture-net", now); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*contract.ActivationVerify){
		"wrong network":  func(value *contract.ActivationVerify) { value.Network = "other" },
		"expired offer":  func(value *contract.ActivationVerify) { value.OfferExpiresAt = contract.Timestamp(now) },
		"invalid wallet": func(value *contract.ActivationVerify) { value.SenderWallet = "short" },
		"invalid amount": func(value *contract.ActivationVerify) { value.Amount = "01.2" },
		"missing bot":    func(value *contract.ActivationVerify) { value.BotID = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if err := ValidateActivation(value, "fixture-net", now); err == nil {
				t.Fatal("invalid activation accepted")
			}
		})
	}
}

func TestValidateTopupResult(t *testing.T) {
	expected := contract.TopupVerify{OperationID: "op", TransactionID: "tx", ExternalReservationID: "reservation", SenderWallet: "sender", ReceiverWallet: "receiver", Amount: "1.2", Asset: "USDT", Network: "network"}
	actual := contract.TopupVerified{OperationID: "op", TransactionID: "tx", ExternalReservationID: "reservation", SenderWallet: "sender", ReceiverWallet: "receiver", Amount: "1.2", Asset: "USDT", Network: "network", FinalizedAt: "2026-08-13T12:00:00Z"}
	if err := ValidateTopupResult(actual, expected); err != nil {
		t.Fatal(err)
	}
	actual.Amount = "1.3"
	if err := ValidateTopupResult(actual, expected); err == nil {
		t.Fatal("mismatched facts accepted")
	}
}
