package security

import (
	"bytes"
	"testing"
)

func TestEncryptBytesRoundTrip(t *testing.T) {
	p := NewPIIProtector(nil, "PII_TEST", "test-seed")
	plain := []byte("proof-file-bytes")

	enc, err := p.EncryptBytes(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(enc, plain) {
		t.Fatalf("expected encrypted payload to differ from plaintext")
	}

	dec, err := p.DecryptBytes(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatalf("expected decrypted payload to round-trip")
	}
}

func TestDeterministicToken_EmbedsVersionMarker(t *testing.T) {
	p := NewPIIProtector(nil, "PII_TEST", "test-seed")
	token, err := p.DeterministicToken("person@example.com|5551234567")
	if err != nil {
		t.Fatal(err)
	}
	if token == "person@example.com|5551234567" {
		t.Fatalf("expected tokenized value, got raw plaintext")
	}
	if len(token) < 5 || token[:3] != "tk1" {
		t.Fatalf("expected versioned deterministic token, got %s", token)
	}
}

func TestIsEncryptedValue_RejectsPlaintextStartingWithK(t *testing.T) {
	p := NewPIIProtector(nil, "PII_TEST", "test-seed")
	if p.IsEncryptedValue("karen") {
		t.Fatalf("expected plaintext value to not be treated as encrypted")
	}
	if p.IsEncryptedValue("k1:not-base64") {
		t.Fatalf("expected malformed envelope to not be treated as encrypted")
	}
	enc, err := p.Encrypt("karen")
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsEncryptedValue(enc) {
		t.Fatalf("expected valid encrypted payload to be recognized")
	}
}

func TestDecryptKeyMaterial_FailsWithWrongMasterKey(t *testing.T) {
	t.Setenv("SECRET_MASTER_KEY", "master-a")
	sealed, err := NewSecretStoreProtector("master-a").EncryptIfNeeded("seed-v1")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SECRET_MASTER_KEY", "master-b")
	p := NewPIIProtector(nil, "PII_TEST", "fallback")
	if _, err := p.decryptKeyMaterial(sealed); err == nil {
		t.Fatalf("expected decryption failure with wrong master key")
	}
}

func TestVerifyActiveKeyMaterial_RoundTrip(t *testing.T) {
	p := NewPIIProtector(nil, "PII_TEST", "test-seed")
	if err := p.VerifyActiveKeyMaterial(); err != nil {
		t.Fatalf("expected key material verification to pass, got %v", err)
	}
}
