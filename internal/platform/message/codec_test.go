package message

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	platformcrypto "github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/dmuraveiko/RW/internal/platform/envelope"
	"github.com/google/uuid"
)

type testKeys struct{ key ed25519.PublicKey }

func (k testKeys) Lookup(_, _ string, _ time.Time) (ed25519.PublicKey, error) { return k.key, nil }

func TestCodecSignAndVerify(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	codec := Codec{Producer: "producer", KeyID: "key-1", PrivateKey: privateKey, Trusted: testKeys{key: privateKey.Public().(ed25519.PublicKey)}, ClockSkew: time.Minute}
	correlationID, _ := uuid.NewV7()
	item, err := codec.Sign("rw.test.v1.action.run", "rw.test.action.run.v1", correlationID, uuid.Nil, map[string]string{"value": "ok"}, time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	verified, err := codec.Verify(item.Envelope, item.Subject, "rw.test.action.run.v1", "producer")
	if err != nil {
		t.Fatal(err)
	}
	if verified.ID != item.MessageID || verified.Envelope.EnvelopeVersion != envelope.Version {
		t.Fatal("verified message does not match signed message")
	}
	if _, err = codec.Verify(item.Envelope, "rw.test.v1.action.other", "rw.test.action.run.v1", "producer"); err == nil {
		t.Fatal("wrong delivery subject accepted")
	}
}

func TestSealAndOpenOutbox(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "keyring.json")
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	contents := `{"version":1,"active_key_id":"test","keys":[{"key_id":"test","key_base64":"` + base64.StdEncoding.EncodeToString(key) + `","decrypt_only":false}]}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	keyring, err := platformcrypto.LoadDataKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	messageID, _ := uuid.NewV7()
	original := OutboxMessage{MessageID: messageID, Subject: "rw.test.v1.action.run", Envelope: []byte("signed-envelope")}
	sealed, err := SealOutbox(original, keyring, "test-service")
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed.Envelope) == string(original.Envelope) {
		t.Fatal("outbox envelope was stored in plaintext")
	}
	opened, err := OpenOutbox(sealed, keyring, "test-service")
	if err != nil {
		t.Fatal(err)
	}
	if string(opened.Envelope) != string(original.Envelope) {
		t.Fatal("outbox envelope did not round-trip")
	}
	if _, err = OpenOutbox(sealed, keyring, "another-service"); err == nil {
		t.Fatal("outbox ciphertext accepted with wrong AAD")
	}
}
