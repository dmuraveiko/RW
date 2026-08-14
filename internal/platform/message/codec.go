package message

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dmuraveiko/RW/internal/platform/envelope"
	"github.com/google/uuid"
)

type Codec struct {
	Producer   string
	KeyID      string
	PrivateKey ed25519.PrivateKey
	Trusted    envelope.KeyLookup
	ClockSkew  time.Duration
}

type Verified struct {
	Envelope envelope.Envelope
	Payload  []byte
	ID       uuid.UUID
	Expires  time.Time
}

func (c Codec) Sign(subject, messageType string, correlationID, causationID uuid.UUID, payload any, expiresAt time.Time) (OutboxMessage, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("encode payload: %w", err)
	}
	messageID, err := uuid.NewV7()
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("generate message ID: %w", err)
	}
	now := time.Now().UTC()
	meta := envelope.Metadata{
		MessageID: messageID.String(), MessageType: messageType, Subject: subject,
		Producer: c.Producer, KeyID: c.KeyID, OccurredAt: now, ExpiresAt: &expiresAt,
		CorrelationID: correlationID.String(),
	}
	if causationID != uuid.Nil {
		meta.CausationID = causationID.String()
	}
	signed, err := envelope.Sign(meta, rawPayload, c.PrivateKey)
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("sign envelope: %w", err)
	}
	encoded, err := envelope.Encode(signed)
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("encode envelope: %w", err)
	}
	return OutboxMessage{MessageID: messageID, Subject: subject, Envelope: encoded, ExpiresAt: expiresAt}, nil
}

func (c Codec) Verify(data []byte, subject, messageType string, allowedProducers ...string) (Verified, error) {
	env, payload, err := envelope.Decode(data, envelope.MaxBytes)
	if err != nil {
		return Verified{}, err
	}
	allowed := make(map[string]struct{}, len(allowedProducers))
	for _, producer := range allowedProducers {
		allowed[producer] = struct{}{}
	}
	if err = envelope.Verify(env, payload, c.Trusted, envelope.VerifyPolicy{Subject: subject, MessageType: messageType, AllowedProducers: allowed, RequireExpiry: true, Now: time.Now().UTC(), ClockSkew: c.ClockSkew, EventMaxAge: 24 * time.Hour}); err != nil {
		return Verified{}, err
	}
	id, err := uuid.Parse(env.MessageID)
	if err != nil {
		return Verified{}, errors.New("invalid verified message ID")
	}
	expires, err := time.Parse(time.RFC3339Nano, env.ExpiresAt)
	if err != nil {
		return Verified{}, errors.New("invalid verified expiry")
	}
	return Verified{Envelope: env, Payload: payload, ID: id, Expires: expires}, nil
}

func DecodePayload[T any](data []byte) (T, error) {
	var result T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode payload: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return result, errors.New("unexpected trailing payload value")
	}
	return result, nil
}
