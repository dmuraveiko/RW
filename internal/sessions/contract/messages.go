package contract

import "time"

const (
	ActivationVerifySubject = "rw.sessions.v1.activation.verify"
	ActivationVerifyType    = "rw.sessions.activation.verify.v1"
	ActivationActivated     = "rw.sessions.v1.activation.activated"
	ActivationActivatedType = "rw.sessions.activation.activated.v1"
	ActivationRejected      = "rw.sessions.v1.activation.rejected"
	ActivationRejectedType  = "rw.sessions.activation.rejected.v1"
	TopupVerifySubject      = "rw.topup.v1.activation.verify"
	TopupVerifyType         = "rw.topup.activation.verify.v1"
	TopupVerifiedSubject    = "rw.topup.v1.activation.verified"
	TopupVerifiedType       = "rw.topup.activation.verified.v1"
	TopupRejectedSubject    = "rw.topup.v1.activation.verification_rejected"
	TopupRejectedType       = "rw.topup.activation.verification_rejected.v1"
)

type ActivationVerify struct {
	OperationID           string `json:"operation_id"`
	SessionID             string `json:"session_id"`
	BalanceID             string `json:"balance_id"`
	BotID                 int64  `json:"bot_id"`
	TelegramUserID        int64  `json:"telegram_user_id"`
	TelegramChatID        int64  `json:"telegram_chat_id"`
	SenderWallet          string `json:"sender_wallet"`
	ReceiverWallet        string `json:"receiver_wallet"`
	Amount                string `json:"amount"`
	Asset                 string `json:"asset"`
	Network               string `json:"network"`
	TransactionID         string `json:"transaction_id"`
	ExternalReservationID string `json:"external_reservation_id"`
	OfferValidFrom        string `json:"offer_valid_from"`
	OfferExpiresAt        string `json:"offer_expires_at"`
	DisplayLabel          string `json:"display_label,omitempty"`
}

type TopupVerify struct {
	OperationID           string `json:"operation_id"`
	TransactionID         string `json:"transaction_id"`
	ExternalReservationID string `json:"external_reservation_id"`
	SenderWallet          string `json:"sender_wallet"`
	ReceiverWallet        string `json:"receiver_wallet"`
	Amount                string `json:"amount"`
	Asset                 string `json:"asset"`
	Network               string `json:"network"`
}

type TopupVerified struct {
	OperationID           string `json:"operation_id"`
	TransactionID         string `json:"transaction_id"`
	ExternalReservationID string `json:"external_reservation_id"`
	SenderWallet          string `json:"sender_wallet"`
	ReceiverWallet        string `json:"receiver_wallet"`
	Amount                string `json:"amount"`
	Asset                 string `json:"asset"`
	Network               string `json:"network"`
	FinalizedAt           string `json:"finalized_at"`
}

type Rejection struct {
	OperationID string `json:"operation_id"`
	Code        string `json:"code"`
	Retryable   bool   `json:"retryable"`
	RetryAfter  string `json:"retry_after,omitempty"`
}

type Activated struct {
	OperationID      string `json:"operation_id"`
	SessionID        string `json:"session_id"`
	BalanceID        string `json:"balance_id"`
	Status           string `json:"status"`
	AuthorityVersion int64  `json:"authority_version"`
	ActivatedAt      string `json:"activated_at"`
}

func Timestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
