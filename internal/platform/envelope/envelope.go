package envelope

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	Version      = 1
	ContentType  = "application/json"
	MaxBytes     = 262144
	signingLabel = "RW-NATS-SIGNED-V1"
)

type Envelope struct {
	EnvelopeVersion int    `json:"envelope_version"`
	MessageID       string `json:"message_id"`
	MessageType     string `json:"message_type"`
	Subject         string `json:"subject"`
	Producer        string `json:"producer"`
	KeyID           string `json:"key_id"`
	OccurredAt      string `json:"occurred_at"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	CorrelationID   string `json:"correlation_id"`
	CausationID     string `json:"causation_id,omitempty"`
	ContentType     string `json:"content_type"`
	PayloadBase64   string `json:"payload_base64"`
	SignatureBase64 string `json:"signature_base64"`
}

type Metadata struct {
	MessageID     string
	MessageType   string
	Subject       string
	Producer      string
	KeyID         string
	OccurredAt    time.Time
	ExpiresAt     *time.Time
	CorrelationID string
	CausationID   string
}

type KeyLookup interface {
	Lookup(producer, keyID string, at time.Time) (ed25519.PublicKey, error)
}

type VerifyPolicy struct {
	Subject          string
	MessageType      string
	AllowedProducers map[string]struct{}
	RequireExpiry    bool
	Now              time.Time
	ClockSkew        time.Duration
	EventMaxAge      time.Duration
}

func Sign(meta Metadata, payload []byte, privateKey ed25519.PrivateKey) (Envelope, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Envelope{}, errors.New("invalid Ed25519 private key")
	}
	env := Envelope{EnvelopeVersion: Version, MessageID: meta.MessageID, MessageType: meta.MessageType, Subject: meta.Subject, Producer: meta.Producer, KeyID: meta.KeyID, OccurredAt: canonicalTime(meta.OccurredAt), CorrelationID: meta.CorrelationID, CausationID: meta.CausationID, ContentType: ContentType, PayloadBase64: base64.StdEncoding.EncodeToString(payload)}
	if meta.ExpiresAt != nil {
		env.ExpiresAt = canonicalTime(*meta.ExpiresAt)
	}
	if _, err := validateFields(env); err != nil {
		return Envelope{}, err
	}
	signature := ed25519.Sign(privateKey, signingInput(env, payload))
	env.SignatureBase64 = base64.StdEncoding.EncodeToString(signature)
	return env, nil
}

func Encode(env Envelope) ([]byte, error) {
	if _, err := validateFields(env); err != nil {
		return nil, err
	}
	if err := validateSignature(env.SignatureBase64); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(env); err != nil {
		return nil, err
	}
	encoded := bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
	if len(encoded) > MaxBytes {
		return nil, errors.New("envelope exceeds maximum size")
	}
	return encoded, nil
}

func Decode(data []byte, maxBytes int) (Envelope, []byte, error) {
	if maxBytes <= 0 || maxBytes > MaxBytes {
		maxBytes = MaxBytes
	}
	if len(data) == 0 || len(data) > maxBytes {
		return Envelope{}, nil, errors.New("envelope size is outside allowed range")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var env Envelope
	if err := decoder.Decode(&env); err != nil {
		return Envelope{}, nil, fmt.Errorf("decode envelope: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Envelope{}, nil, err
	}
	payload, err := validateFields(env)
	if err != nil {
		return Envelope{}, nil, err
	}
	if err := validateSignature(env.SignatureBase64); err != nil {
		return Envelope{}, nil, err
	}
	return env, payload, nil
}

func Verify(env Envelope, payload []byte, keys KeyLookup, policy VerifyPolicy) error {
	_, err := validateFields(env)
	if err != nil {
		return err
	}
	if err := validateSignature(env.SignatureBase64); err != nil {
		return err
	}
	parsedOccurred, _ := parseCanonicalTime(env.OccurredAt)
	if env.Subject != policy.Subject {
		return errors.New("envelope subject does not match delivery subject")
	}
	if env.MessageType != policy.MessageType {
		return errors.New("message type is not allowed")
	}
	if _, ok := policy.AllowedProducers[env.Producer]; !ok {
		return errors.New("producer is not allowed")
	}
	if policy.RequireExpiry && env.ExpiresAt == "" {
		return errors.New("command expiry is required")
	}
	now := policy.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if parsedOccurred.After(now.Add(policy.ClockSkew)) {
		return errors.New("message occurred_at is in the future")
	}
	if policy.EventMaxAge > 0 && parsedOccurred.Before(now.Add(-policy.EventMaxAge)) {
		return errors.New("event is too old")
	}
	if env.ExpiresAt != "" {
		expires, _ := parseCanonicalTime(env.ExpiresAt)
		if now.After(expires.Add(policy.ClockSkew)) {
			return errors.New("message has expired")
		}
	}
	publicKey, err := keys.Lookup(env.Producer, env.KeyID, parsedOccurred)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(env.SignatureBase64)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid signature encoding")
	}
	if !ed25519.Verify(publicKey, signingInput(env, payload), signature) {
		return errors.New("invalid envelope signature")
	}
	return nil
}

func validateFields(env Envelope) ([]byte, error) {
	if env.EnvelopeVersion != Version {
		return nil, errors.New("unsupported envelope version")
	}
	if err := uuidV7(env.MessageID); err != nil {
		return nil, fmt.Errorf("message_id: %w", err)
	}
	if err := uuidV7(env.CorrelationID); err != nil {
		return nil, fmt.Errorf("correlation_id: %w", err)
	}
	if env.CausationID != "" {
		if err := uuidV7(env.CausationID); err != nil {
			return nil, fmt.Errorf("causation_id: %w", err)
		}
	}
	if env.MessageType == "" || env.Subject == "" || env.Producer == "" || env.KeyID == "" {
		return nil, errors.New("message identity fields are required")
	}
	if env.ContentType != ContentType {
		return nil, errors.New("unsupported content type")
	}
	if _, err := parseCanonicalTime(env.OccurredAt); err != nil {
		return nil, fmt.Errorf("occurred_at: %w", err)
	}
	if env.ExpiresAt == "" {
		return nil, errors.New("expires_at is required in envelope v1")
	}
	if env.ExpiresAt != "" {
		expiresAt, err := parseCanonicalTime(env.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("expires_at: %w", err)
		}
		occurredAt, _ := parseCanonicalTime(env.OccurredAt)
		if !expiresAt.After(occurredAt) {
			return nil, errors.New("expires_at must be after occurred_at")
		}
	}
	payload, err := base64.StdEncoding.DecodeString(env.PayloadBase64)
	if err != nil {
		return nil, errors.New("invalid payload base64")
	}
	return payload, nil
}

func validateSignature(value string) error {
	signature, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid signature encoding")
	}
	return nil
}

func signingInput(env Envelope, payload []byte) []byte {
	parts := [][]byte{[]byte(strconv.Itoa(env.EnvelopeVersion)), []byte(env.MessageID), []byte(env.MessageType), []byte(env.Subject), []byte(env.Producer), []byte(env.KeyID), []byte(env.OccurredAt), []byte(env.ExpiresAt), []byte(env.CorrelationID), []byte(env.CausationID), []byte(env.ContentType), payload}
	size := len(signingLabel)
	for _, part := range parts {
		size += 8 + len(part)
	}
	result := make([]byte, 0, size)
	result = append(result, signingLabel...)
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		result = append(result, length[:]...)
		result = append(result, part...)
	}
	return result
}

func parseCanonicalTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || canonicalTime(parsed) != value {
		return time.Time{}, errors.New("timestamp must be canonical UTC RFC3339Nano")
	}
	return parsed, nil
}

func canonicalTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func uuidV7(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value || parsed.Version() != 7 {
		return errors.New("must be a lowercase canonical UUIDv7")
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	}
	return errors.New("unexpected trailing JSON value")
}
