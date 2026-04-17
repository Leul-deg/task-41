package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

const secretEnvelopePrefix = "es1:"

type SecretStoreProtector struct {
	masterKey string
}

func NewSecretStoreProtector(masterKey string) *SecretStoreProtector {
	return &SecretStoreProtector{masterKey: strings.TrimSpace(masterKey)}
}

func (p *SecretStoreProtector) EncryptIfNeeded(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, secretEnvelopePrefix) {
		return trimmed, nil
	}
	if strings.TrimSpace(p.masterKey) == "" {
		return "", errors.New("secret master key is required")
	}

	block, err := aes.NewCipher(deriveKey(p.masterKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, []byte(trimmed), nil)
	combined := append(nonce, ct...)
	return secretEnvelopePrefix + base64.RawStdEncoding.EncodeToString(combined), nil
}

func (p *SecretStoreProtector) DecryptIfNeeded(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if !strings.HasPrefix(trimmed, secretEnvelopePrefix) {
		return trimmed, nil
	}
	if strings.TrimSpace(p.masterKey) == "" {
		return "", errors.New("secret master key is required")
	}

	raw := strings.TrimPrefix(trimmed, secretEnvelopePrefix)
	decoded, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(deriveKey(p.masterKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	n := gcm.NonceSize()
	if len(decoded) < n {
		return "", errors.New("encrypted secret payload too short")
	}
	nonce := decoded[:n]
	ct := decoded[n:]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
