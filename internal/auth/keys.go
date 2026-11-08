package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// Keys derives purpose-specific subkeys from the master key kept only in the
// Pi's environment file (ADR 0004).
type Keys struct {
	aead   cipher.AEAD
	macKey []byte
}

func NewKeys(masterHex string) (*Keys, error) {
	master, err := hex.DecodeString(masterHex)
	if err != nil || len(master) != 32 {
		return nil, errors.New("BLOG_TOTP_KEY must be 64 hex characters (32 bytes); generate with: openssl rand -hex 32")
	}
	block, err := aes.NewCipher(derive(master, "blog/totp-secret-encryption"))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Keys{aead: aead, macKey: derive(master, "blog/recovery-code-mac")}, nil
}

func derive(master []byte, label string) []byte {
	m := hmac.New(sha256.New, master)
	m.Write([]byte(label))
	return m.Sum(nil)
}

// Seal encrypts plaintext with AES-256-GCM; the result is nonce||ciphertext.
func (k *Keys) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return k.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (k *Keys) Open(sealed []byte) ([]byte, error) {
	n := k.aead.NonceSize()
	if len(sealed) < n {
		return nil, fmt.Errorf("sealed value too short")
	}
	return k.aead.Open(nil, sealed[:n], sealed[n:], nil)
}

// MAC returns a keyed hash, so stored recovery codes cannot be brute-forced
// offline from a leaked backup.
func (k *Keys) MAC(value string) string {
	m := hmac.New(sha256.New, k.macKey)
	m.Write([]byte(value))
	return hex.EncodeToString(m.Sum(nil))
}
