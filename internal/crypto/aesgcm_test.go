package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestAESGCM_Roundtrip(t *testing.T) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("key: %v", err)
	}
	a, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	plain := []byte("gsk_secret_groq_api_key_value_42")
	ct, nonce, err := a.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ct, plain) {
		t.Fatalf("ciphertext must differ from plaintext")
	}

	got, err := a.Decrypt(ct, nonce)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round-trip mismatch: %q vs %q", got, plain)
	}
}

func TestAESGCM_TamperedCiphertextFails(t *testing.T) {
	key := make([]byte, KeySize)
	rand.Read(key)
	a, _ := New(key)

	ct, nonce, _ := a.Encrypt([]byte("hello"))
	ct[0] ^= 0xFF

	if _, err := a.Decrypt(ct, nonce); err == nil {
		t.Fatalf("expected decrypt failure on tampered ciphertext")
	}
}

func TestAESGCM_NewFromBase64(t *testing.T) {
	key := make([]byte, KeySize)
	rand.Read(key)
	encoded := base64.StdEncoding.EncodeToString(key)

	a, err := NewFromBase64(encoded)
	if err != nil {
		t.Fatalf("NewFromBase64: %v", err)
	}
	ct, nonce, _ := a.Encrypt([]byte("x"))
	if _, err := a.Decrypt(ct, nonce); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
}

func TestAESGCM_RejectsShortKey(t *testing.T) {
	if _, err := New(make([]byte, 16)); err == nil {
		t.Fatalf("expected error for non-32-byte key")
	}
}
