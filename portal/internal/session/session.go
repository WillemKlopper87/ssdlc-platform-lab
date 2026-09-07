// portal/internal/session/session.go
package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Encode seals token with AES-256-GCM under key, returning a URL-safe
// base64 string suitable for a cookie value. The session cookie holds a
// real Gitea access token, not a portal-issued identifier — sealing it
// (not just signing it) matters because leaking the cookie must not leak
// the token in plaintext to anything inspecting cookie storage.
func Encode(token string, key []byte) (string, error) {
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return "", fmt.Errorf("session: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("session: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("session: nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(token), nil)
	return base64.URLEncoding.EncodeToString(sealed), nil
}

// Decode reverses Encode. Any failure — wrong key, truncated or tampered
// ciphertext — is returned as a generic error; callers must treat any
// error as "not authenticated", never attempt partial recovery.
func Decode(value string, key []byte) (string, error) {
	raw, err := base64.URLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("session: bad encoding: %w", err)
	}
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return "", fmt.Errorf("session: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("session: new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("session: ciphertext too short")
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("session: open: %w", err)
	}
	return string(plain), nil
}
