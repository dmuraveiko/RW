package envelope

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"
)

type fixture struct {
	PrivateKeySeed string   `json:"private_key_seed_base64"`
	PublicKey      string   `json:"public_key_base64"`
	RawPayload     string   `json:"raw_payload"`
	Envelope       Envelope `json:"envelope"`
}

type staticKeys struct{ key ed25519.PublicKey }

func (s staticKeys) Lookup(string, string, time.Time) (ed25519.PublicKey, error) { return s.key, nil }

func TestGoldenSignatureVector(t *testing.T) {
	vector := readFixture(t)
	seed, err := base64.StdEncoding.DecodeString(vector.PrivateKeySeed)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	occurredAt, _ := time.Parse(time.RFC3339Nano, vector.Envelope.OccurredAt)
	expiresAt, _ := time.Parse(time.RFC3339Nano, vector.Envelope.ExpiresAt)
	actual, err := Sign(Metadata{MessageID: vector.Envelope.MessageID, MessageType: vector.Envelope.MessageType, Subject: vector.Envelope.Subject, Producer: vector.Envelope.Producer, KeyID: vector.Envelope.KeyID, OccurredAt: occurredAt, ExpiresAt: &expiresAt, CorrelationID: vector.Envelope.CorrelationID}, []byte(vector.RawPayload), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if actual.SignatureBase64 != vector.Envelope.SignatureBase64 {
		t.Fatalf("signature mismatch\nwant %s\ngot  %s", vector.Envelope.SignatureBase64, actual.SignatureBase64)
	}
	encoded, err := Encode(actual)
	if err != nil {
		t.Fatal(err)
	}
	decoded, payload, err := Decode(encoded, 262144)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != vector.RawPayload {
		t.Fatalf("payload changed: %q", payload)
	}
	publicKey, _ := base64.StdEncoding.DecodeString(vector.PublicKey)
	err = Verify(decoded, payload, staticKeys{key: ed25519.PublicKey(publicKey)}, VerifyPolicy{Subject: decoded.Subject, MessageType: decoded.MessageType, AllowedProducers: map[string]struct{}{decoded.Producer: {}}, RequireExpiry: true, Now: occurredAt.Add(time.Minute), ClockSkew: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	vector := readFixture(t)
	publicKey, _ := base64.StdEncoding.DecodeString(vector.PublicKey)
	now, _ := time.Parse(time.RFC3339Nano, "2026-08-10T12:01:00Z")
	err := Verify(vector.Envelope, []byte(`{"balance_id":"another"}`), staticKeys{key: ed25519.PublicKey(publicKey)}, VerifyPolicy{Subject: vector.Envelope.Subject, MessageType: vector.Envelope.MessageType, AllowedProducers: map[string]struct{}{vector.Envelope.Producer: {}}, RequireExpiry: true, Now: now, ClockSkew: 2 * time.Minute})
	if err == nil || err.Error() != "invalid envelope signature" {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	_, _, err := Decode([]byte(`{"envelope_version":1,"unknown":true}`), 262144)
	if err == nil {
		t.Fatal("expected strict decoder error")
	}
}

func readFixture(t *testing.T) fixture {
	t.Helper()
	contents, err := os.ReadFile("../../../contracts/fixtures/envelope-v1-positive.json")
	if err != nil {
		t.Fatal(err)
	}
	var result fixture
	if err = json.Unmarshal(contents, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
