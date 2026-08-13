package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TrustedKey struct {
	Producer   string
	KeyID      string
	PublicKey  ed25519.PublicKey
	ValidFrom  time.Time
	ValidUntil time.Time
}

type TrustedKeys struct {
	keys map[string]TrustedKey
}

type DataKeyring struct {
	active string
	keys   map[string]dataKey
}

type dataKey struct {
	key         []byte
	decryptOnly bool
}

type Ciphertext struct {
	KeyID string `json:"key_id"`
	Nonce string `json:"nonce_base64"`
	Value string `json:"ciphertext_base64"`
}

type trustedKeysFile struct {
	Version int `json:"version"`
	Keys    []struct {
		Producer     string `json:"producer"`
		KeyID        string `json:"key_id"`
		PublicKeyPEM string `json:"public_key_pem"`
		ValidFrom    string `json:"valid_from"`
		ValidUntil   string `json:"valid_until"`
	} `json:"keys"`
}

type dataKeyringFile struct {
	Version     int    `json:"version"`
	ActiveKeyID string `json:"active_key_id"`
	Keys        []struct {
		KeyID       string `json:"key_id"`
		KeyBase64   string `json:"key_base64"`
		DecryptOnly bool   `json:"decrypt_only"`
	} `json:"keys"`
}

func LoadSigningPrivateKey(filePath, encoded string) (ed25519.PrivateKey, error) {
	var raw []byte
	var err error
	if filePath != "" {
		if err = validateFile(filePath); err != nil {
			return nil, err
		}
		raw, err = os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read signing key: %w", err)
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, errors.New("signing key must be PKCS#8 PEM")
		}
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse signing key: %w", parseErr)
		}
		key, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, errors.New("signing key is not Ed25519")
		}
		return key, nil
	}
	raw, err = base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("decode signing private key")
	}
	if parsed, parseErr := x509.ParsePKCS8PrivateKey(raw); parseErr == nil {
		if key, ok := parsed.(ed25519.PrivateKey); ok {
			return key, nil
		}
	}
	if len(raw) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(raw), nil
	}
	if len(raw) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(raw), nil
	}
	return nil, errors.New("test signing key must be Ed25519 seed, private key or PKCS#8 DER")
}

func LoadTrustedKeys(path string) (TrustedKeys, error) {
	if err := validateFile(path); err != nil {
		return TrustedKeys{}, err
	}
	var manifest trustedKeysFile
	if err := decodeFile(path, &manifest); err != nil {
		return TrustedKeys{}, fmt.Errorf("trusted keys: %w", err)
	}
	if manifest.Version != 1 || len(manifest.Keys) == 0 {
		return TrustedKeys{}, errors.New("trusted keys manifest must be non-empty version 1")
	}
	result := TrustedKeys{keys: make(map[string]TrustedKey, len(manifest.Keys))}
	for _, item := range manifest.Keys {
		block, _ := pem.Decode([]byte(item.PublicKeyPEM))
		if block == nil {
			return TrustedKeys{}, fmt.Errorf("trusted key %q is not PEM", item.KeyID)
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return TrustedKeys{}, fmt.Errorf("parse trusted key %q: %w", item.KeyID, err)
		}
		publicKey, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return TrustedKeys{}, fmt.Errorf("trusted key %q is not Ed25519", item.KeyID)
		}
		validFrom, err := time.Parse(time.RFC3339, item.ValidFrom)
		if err != nil {
			return TrustedKeys{}, fmt.Errorf("parse valid_from for %q", item.KeyID)
		}
		validUntil, err := time.Parse(time.RFC3339, item.ValidUntil)
		if err != nil || !validUntil.After(validFrom) {
			return TrustedKeys{}, fmt.Errorf("invalid validity for %q", item.KeyID)
		}
		if item.Producer == "" || item.KeyID == "" {
			return TrustedKeys{}, errors.New("trusted producer and key ID are required")
		}
		mapKey := item.Producer + "\x00" + item.KeyID
		if _, exists := result.keys[mapKey]; exists {
			return TrustedKeys{}, fmt.Errorf("duplicate trusted key %s/%s", item.Producer, item.KeyID)
		}
		result.keys[mapKey] = TrustedKey{Producer: item.Producer, KeyID: item.KeyID, PublicKey: publicKey, ValidFrom: validFrom, ValidUntil: validUntil}
	}
	return result, nil
}

func (k TrustedKeys) Lookup(producer, keyID string, at time.Time) (ed25519.PublicKey, error) {
	key, ok := k.keys[producer+"\x00"+keyID]
	if !ok {
		return nil, errors.New("untrusted producer or key ID")
	}
	if at.Before(key.ValidFrom) || !at.Before(key.ValidUntil) {
		return nil, errors.New("trusted key is outside its validity period")
	}
	return key.PublicKey, nil
}

func LoadDataKeyring(path string) (DataKeyring, error) {
	if err := validateFile(path); err != nil {
		return DataKeyring{}, err
	}
	var manifest dataKeyringFile
	if err := decodeFile(path, &manifest); err != nil {
		return DataKeyring{}, fmt.Errorf("data keyring: %w", err)
	}
	if manifest.Version != 1 || manifest.ActiveKeyID == "" {
		return DataKeyring{}, errors.New("data keyring must be version 1 with an active key")
	}
	result := DataKeyring{active: manifest.ActiveKeyID, keys: make(map[string]dataKey, len(manifest.Keys))}
	activeCount := 0
	for _, item := range manifest.Keys {
		if item.KeyID == "" {
			return DataKeyring{}, errors.New("empty data key ID")
		}
		if _, exists := result.keys[item.KeyID]; exists {
			return DataKeyring{}, fmt.Errorf("duplicate data key %q", item.KeyID)
		}
		key, err := base64.StdEncoding.DecodeString(item.KeyBase64)
		if err != nil || len(key) != 32 {
			return DataKeyring{}, fmt.Errorf("data key %q must decode to 32 bytes", item.KeyID)
		}
		result.keys[item.KeyID] = dataKey{key: key, decryptOnly: item.DecryptOnly}
		if item.KeyID == manifest.ActiveKeyID {
			activeCount++
			if item.DecryptOnly {
				return DataKeyring{}, errors.New("active data key cannot be decrypt-only")
			}
		}
	}
	if activeCount != 1 {
		return DataKeyring{}, errors.New("active data key must exist exactly once")
	}
	return result, nil
}

func (k DataKeyring) Encrypt(plaintext, aad []byte) (Ciphertext, error) {
	key, ok := k.keys[k.active]
	if !ok || key.decryptOnly {
		return Ciphertext{}, errors.New("active encryption key unavailable")
	}
	block, err := aes.NewCipher(key.key)
	if err != nil {
		return Ciphertext{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Ciphertext{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return Ciphertext{}, fmt.Errorf("generate encryption nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, plaintext, aad)
	return Ciphertext{KeyID: k.active, Nonce: base64.StdEncoding.EncodeToString(nonce), Value: base64.StdEncoding.EncodeToString(sealed)}, nil
}

func (k DataKeyring) Decrypt(value Ciphertext, aad []byte) ([]byte, error) {
	key, ok := k.keys[value.KeyID]
	if !ok {
		return nil, errors.New("unknown data key ID")
	}
	nonce, err := base64.StdEncoding.DecodeString(value.Nonce)
	if err != nil {
		return nil, errors.New("decode encryption nonce")
	}
	sealed, err := base64.StdEncoding.DecodeString(value.Value)
	if err != nil {
		return nil, errors.New("decode ciphertext")
	}
	block, err := aes.NewCipher(key.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid encryption nonce length")
	}
	plain, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, errors.New("decrypt data")
	}
	return plain, nil
}

func LoadFingerprintKey(path string) ([]byte, error) {
	if err := validateFile(path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fingerprint key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, errors.New("fingerprint key must be standard base64 containing 32 bytes")
	}
	return key, nil
}

func Fingerprint(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func validateFile(path string) error {
	clean := filepath.Clean(path)
	info, err := os.Lstat(clean)
	if err != nil {
		return fmt.Errorf("inspect secret file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("secret file must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return errors.New("secret file must be regular")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("secret file must not be group/world-writable")
	}
	return nil
}

func decodeFile(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("unexpected trailing JSON value")
	}
	return nil
}
