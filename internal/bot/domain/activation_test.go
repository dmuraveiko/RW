package domain

import (
	"testing"
	"time"

	"github.com/dmuraveiko/RW/internal/bot/contract"
)

func TestFormatAmount(t *testing.T) {
	for _, test := range []struct {
		minor int64
		scale int
		want  string
	}{{1001, 6, "0.001001"}, {1234567, 6, "1.234567"}, {42, 0, "42"}} {
		if got := FormatAmount(test.minor, test.scale); got != test.want {
			t.Fatalf("FormatAmount(%d, %d) = %q, want %q", test.minor, test.scale, got, test.want)
		}
	}
}

func TestValidateReserved(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	result := contract.ActivationReserved{OperationID: "0198a123-4567-7abc-8def-0123456789ab", ExternalReservationID: "reservation-1", ReceiverWallet: "0x9999999999999999999999999999999999999999", Amount: "0.001001", ValidFrom: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339Nano)}
	if err := ValidateReserved(result, result.OperationID, result.Amount, now); err != nil {
		t.Fatal(err)
	}
	result.Amount = "0.001002"
	if err := ValidateReserved(result, result.OperationID, "0.001001", now); err == nil {
		t.Fatal("amount mismatch accepted")
	}
}

func TestValidateWallet(t *testing.T) {
	if err := ValidateWallet("0x1111111111111111111111111111111111111111"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"short", " wallet-with-space", "wallet with space"} {
		if err := ValidateWallet(value); err == nil {
			t.Fatalf("invalid wallet %q accepted", value)
		}
	}
}
