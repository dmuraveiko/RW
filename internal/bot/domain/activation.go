package domain

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/dmuraveiko/RW/internal/bot/contract"
	"github.com/google/uuid"
)

var ErrInvalidInput = errors.New("invalid activation input")

func ValidateInviteCreate(command contract.InviteCreate) error {
	if !uuidV7(command.OperationID) || strings.TrimSpace(command.BalanceID) != command.BalanceID || len(command.BalanceID) < 1 || len(command.BalanceID) > 256 {
		return ErrInvalidInput
	}
	if command.RequestedTTLSecond != 0 && (command.RequestedTTLSecond < 300 || command.RequestedTTLSecond > 604800) {
		return ErrInvalidInput
	}
	return nil
}

func ValidateWallet(value string) error {
	if len(value) < 16 || len(value) > 128 || strings.TrimSpace(value) != value {
		return ErrInvalidInput
	}
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e {
			return ErrInvalidInput
		}
	}
	return nil
}

func ValidateReserved(result contract.ActivationReserved, expectedOperationID, expectedAmount string, now time.Time) error {
	if result.OperationID != expectedOperationID || result.Amount != expectedAmount || !externalID(result.ExternalReservationID) || ValidateWallet(result.ReceiverWallet) != nil {
		return ErrInvalidInput
	}
	validFrom, err := time.Parse(time.RFC3339Nano, result.ValidFrom)
	if err != nil {
		return ErrInvalidInput
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, result.ExpiresAt)
	if err != nil || validFrom.After(now.UTC().Add(2*time.Minute)) || !expiresAt.After(now.UTC()) || !expiresAt.After(validFrom) || expiresAt.Sub(validFrom) < time.Minute || expiresAt.Sub(validFrom) > time.Hour {
		return ErrInvalidInput
	}
	return nil
}

func ValidatePayment(result contract.PaymentConfirmed, expected contract.PaymentConfirmed, offerExpiresAt, now time.Time) error {
	if result.OperationID != expected.OperationID || result.ExternalReservationID != expected.ExternalReservationID || result.SenderWallet != expected.SenderWallet || result.ReceiverWallet != expected.ReceiverWallet || result.Amount != expected.Amount || !externalID(result.TransactionID) {
		return ErrInvalidInput
	}
	observedAt, err := time.Parse(time.RFC3339Nano, result.ObservedAt)
	if err != nil || observedAt.After(offerExpiresAt) || observedAt.After(now.UTC().Add(2*time.Minute)) {
		return ErrInvalidInput
	}
	return nil
}

func FormatAmount(minor int64, scale int) string {
	digits := strconv.FormatInt(minor, 10)
	if scale == 0 {
		return digits
	}
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	point := len(digits) - scale
	return digits[:point] + "." + digits[point:]
}

func externalID(value string) bool {
	return len(value) >= 1 && len(value) <= 256 && strings.TrimSpace(value) == value
}

func uuidV7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}
