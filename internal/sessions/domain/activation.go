package domain

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dmuraveiko/RW/internal/sessions/contract"
	"github.com/google/uuid"
)

var ErrInvalidActivation = errors.New("invalid activation verification")

func ValidateActivation(command contract.ActivationVerify, configuredNetwork string, now time.Time) error {
	if !uuidV7(command.OperationID) || !uuidV7(command.SessionID) || command.BotID <= 0 || command.TelegramUserID <= 0 || command.TelegramChatID <= 0 {
		return ErrInvalidActivation
	}
	if len(command.BalanceID) < 1 || len(command.BalanceID) > 256 || command.Asset != "USDT" || command.Network != configuredNetwork {
		return ErrInvalidActivation
	}
	if !validWallet(command.SenderWallet) || !validWallet(command.ReceiverWallet) || !validExternalID(command.TransactionID) || !validExternalID(command.ExternalReservationID) {
		return ErrInvalidActivation
	}
	if !validAmount(command.Amount) || len(command.DisplayLabel) > 128 || !utf8.ValidString(command.DisplayLabel) {
		return ErrInvalidActivation
	}
	validFrom, err := time.Parse(time.RFC3339Nano, command.OfferValidFrom)
	if err != nil {
		return ErrInvalidActivation
	}
	expires, err := time.Parse(time.RFC3339Nano, command.OfferExpiresAt)
	if err != nil || validFrom.After(now.UTC().Add(2*time.Minute)) || !expires.After(now.UTC()) || !expires.After(validFrom) || expires.Sub(validFrom) < time.Minute || expires.Sub(validFrom) > time.Hour || expires.After(now.UTC().Add(2*time.Hour)) {
		return ErrInvalidActivation
	}
	return nil
}

func ValidateTopupResult(result contract.TopupVerified, expected contract.TopupVerify) error {
	if result.OperationID != expected.OperationID || result.TransactionID != expected.TransactionID || result.ExternalReservationID != expected.ExternalReservationID || result.SenderWallet != expected.SenderWallet || result.ReceiverWallet != expected.ReceiverWallet || result.Amount != expected.Amount || result.Asset != expected.Asset || result.Network != expected.Network {
		return errors.New("verified transaction facts do not match expected values")
	}
	if _, err := time.Parse(time.RFC3339Nano, result.FinalizedAt); err != nil {
		return errors.New("invalid finalized timestamp")
	}
	return nil
}

func validWallet(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func validExternalID(value string) bool {
	return len(value) >= 1 && len(value) <= 256 && strings.TrimSpace(value) == value
}

func validAmount(value string) bool {
	if value == "" || len(value) > 64 || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "eE ") {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts[0]) > 1 && parts[0][0] == '0') {
		return false
	}
	if _, err := strconv.ParseUint(parts[0], 10, 64); err != nil {
		return false
	}
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 18 {
			return false
		}
		if _, err := strconv.ParseUint(parts[1], 10, 64); err != nil {
			return false
		}
	}
	return true
}

func uuidV7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}
