package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDataKeyringRoundTripAndAAD(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "keyring.json")
	writeJSON(t, path, map[string]any{"version": 1, "active_key_id": "data-1", "keys": []map[string]any{{"key_id": "data-1", "key_base64": base64.StdEncoding.EncodeToString(make([]byte, 32)), "decrypt_only": false}}})
	keyring, err := LoadDataKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := keyring.Encrypt([]byte("secret"), []byte("row-1"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := keyring.Decrypt(encrypted, []byte("row-1"))
	if err != nil || string(plain) != "secret" {
		t.Fatalf("round trip failed: %q, %v", plain, err)
	}
	if _, err = keyring.Decrypt(encrypted, []byte("row-2")); err == nil {
		t.Fatal("AAD tampering must fail")
	}
}

func TestTrustedKeysValidity(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "trusted.json")
	writeJSON(t, path, map[string]any{"version": 1, "keys": []map[string]any{{"producer": "rw-bot", "key_id": "bot-1", "public_key_pem": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})), "valid_from": "2026-01-01T00:00:00Z", "valid_until": "2027-01-01T00:00:00Z"}}})
	keys, err := LoadTrustedKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	at, _ := time.Parse(time.RFC3339, "2026-08-10T00:00:00Z")
	if _, err = keys.Lookup("rw-bot", "bot-1", at); err != nil {
		t.Fatal(err)
	}
	if _, err = keys.Lookup("rw-bot", "bot-1", at.AddDate(1, 0, 0)); err == nil {
		t.Fatal("expired key must fail")
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
