// portal/internal/session/session_test.go
package session

import "testing"

func TestEncodeDecodeRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	token := "gto_realistictokenvalue1234567890abcdef"

	encoded, err := Encode(token, key)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if encoded == token {
		t.Fatal("Encode returned the plaintext token unchanged — it must be encrypted")
	}

	decoded, err := Decode(encoded, key)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded != token {
		t.Errorf("Decode = %q, want %q", decoded, token)
	}
}

func TestDecode_WrongKeyFails(t *testing.T) {
	key1 := []byte("0123456789abcdef0123456789abcdef")
	key2 := []byte("fedcba9876543210fedcba9876543210")

	encoded, _ := Encode("secret-token", key1)
	if _, err := Decode(encoded, key2); err == nil {
		t.Fatal("expected Decode with the wrong key to fail, got nil error")
	}
}

func TestDecode_TamperedCiphertextFails(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encoded, _ := Encode("secret-token", key)
	tampered := encoded[:len(encoded)-2] + "xx"
	if _, err := Decode(tampered, key); err == nil {
		t.Fatal("expected Decode of tampered ciphertext to fail, got nil error")
	}
}
