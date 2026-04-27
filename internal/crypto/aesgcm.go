package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const KeySize = 32 // AES-256

type AESGCM struct {
	aead cipher.AEAD
}

// NewFromBase64 decodes a 32-byte master key from a standard base64 string.
func NewFromBase64(b64 string) (*AESGCM, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode master key: %w", err)
	}
	return New(raw)
}

func New(key []byte) (*AESGCM, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("crypto: master key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &AESGCM{aead: aead}, nil
}

func (a *AESGCM) Encrypt(plain []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, a.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("crypto: nonce: %w", err)
	}
	ciphertext = a.aead.Seal(nil, nonce, plain, nil)
	return ciphertext, nonce, nil
}

func (a *AESGCM) Decrypt(ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != a.aead.NonceSize() {
		return nil, errors.New("crypto: invalid nonce length")
	}
	plain, err := a.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plain, nil
}
