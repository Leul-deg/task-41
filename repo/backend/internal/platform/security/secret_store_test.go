package security

import "testing"

func TestSecretStoreProtector_EncryptDecryptRoundTrip(t *testing.T) {
	p := NewSecretStoreProtector("master-key-seed")

	enc, err := p.EncryptIfNeeded("plain-secret")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "plain-secret" {
		t.Fatalf("expected encrypted value, got plaintext")
	}

	dec, err := p.DecryptIfNeeded(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != "plain-secret" {
		t.Fatalf("expected decrypted secret, got %s", dec)
	}
}

func TestSecretStoreProtector_DecryptPlaintextPassthrough(t *testing.T) {
	p := NewSecretStoreProtector("master-key-seed")

	dec, err := p.DecryptIfNeeded("legacy-plain")
	if err != nil {
		t.Fatal(err)
	}
	if dec != "legacy-plain" {
		t.Fatalf("expected plaintext passthrough, got %s", dec)
	}
}
