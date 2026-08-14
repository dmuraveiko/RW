package contract

const (
	InviteCreateSubject       = "rw.bot.v1.invite.create"
	InviteCreateType          = "rw.bot.invite.create.v1"
	InviteCreatedSubject      = "rw.bot.v1.invite.created"
	InviteCreatedType         = "rw.bot.invite.created.v1"
	ActivationReserveSubject  = "rw.topup.v1.activation.reserve"
	ActivationReserveType     = "rw.topup.activation.reserve.v1"
	ActivationReservedSubject = "rw.topup.v1.activation.reserved"
	ActivationReservedType    = "rw.topup.activation.reserved.v1"
	ReserveRejectedSubject    = "rw.topup.v1.activation.reserve_rejected"
	ReserveRejectedType       = "rw.topup.activation.reserve_rejected.v1"
	PaymentConfirmedSubject   = "rw.topup.v1.activation.payment_confirmed"
	PaymentConfirmedType      = "rw.topup.activation.payment_confirmed.v1"
)

type InviteCreate struct {
	OperationID        string `json:"operation_id"`
	BalanceID          string `json:"balance_id"`
	RequestedTTLSecond int64  `json:"requested_ttl_seconds,omitempty"`
}

type InviteCreated struct {
	OperationID string `json:"operation_id"`
	Invite      string `json:"invite"`
	BotDeepLink string `json:"bot_deep_link"`
	ExpiresAt   string `json:"expires_at"`
}

type ActivationReserve struct {
	OperationID        string `json:"operation_id"`
	BalanceID          string `json:"balance_id"`
	SessionID          string `json:"session_id"`
	SenderWallet       string `json:"sender_wallet"`
	VerificationAmount string `json:"verification_amount"`
	Asset              string `json:"asset"`
	Network            string `json:"network"`
}

type ActivationReserved struct {
	OperationID           string `json:"operation_id"`
	ExternalReservationID string `json:"external_reservation_id"`
	ReceiverWallet        string `json:"receiver_wallet"`
	Amount                string `json:"amount"`
	ValidFrom             string `json:"valid_from"`
	ExpiresAt             string `json:"expires_at"`
}

type PaymentConfirmed struct {
	OperationID           string `json:"operation_id"`
	ExternalReservationID string `json:"external_reservation_id"`
	TransactionID         string `json:"transaction_id"`
	SenderWallet          string `json:"sender_wallet"`
	ReceiverWallet        string `json:"receiver_wallet"`
	Amount                string `json:"amount"`
	ObservedAt            string `json:"observed_at"`
}

type Rejection struct {
	OperationID string `json:"operation_id"`
	Code        string `json:"code"`
	Retryable   bool   `json:"retryable"`
	RetryAfter  string `json:"retry_after,omitempty"`
}
