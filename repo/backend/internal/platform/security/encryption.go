package security

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type PIIProtector struct {
	DB           *sql.DB
	KeyName      string
	FallbackSeed string
}

func NewPIIProtector(db *sql.DB, keyName, fallbackSeed string) *PIIProtector {
	return &PIIProtector{DB: db, KeyName: keyName, FallbackSeed: fallbackSeed}
}

func (p *PIIProtector) Encrypt(plaintext string) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return "", nil
	}
	version, keyMaterial, err := p.activeKey(context.Background())
	if err != nil {
		return "", err
	}
	ciphertext, err := seal(keyMaterial, plaintext)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("k%d:%s", version, ciphertext), nil
}

func (p *PIIProtector) EncryptBytes(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	encoded, err := p.Encrypt(base64.RawStdEncoding.EncodeToString(plaintext))
	if err != nil {
		return nil, err
	}
	return []byte(encoded), nil
}

func (p *PIIProtector) Decrypt(stored string) (string, error) {
	if strings.TrimSpace(stored) == "" {
		return "", nil
	}
	v, payload, err := parseEncryptedEnvelope(stored)
	if err != nil {
		return "", err
	}
	keyMaterial, err := p.keyByVersion(context.Background(), v)
	if err != nil {
		return "", err
	}
	return open(keyMaterial, payload)
}

func (p *PIIProtector) DecryptBytes(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		return nil, nil
	}
	decoded, err := p.Decrypt(string(stored))
	if err != nil {
		return nil, err
	}
	return base64.RawStdEncoding.DecodeString(decoded)
}

func (p *PIIProtector) IsEncryptedValue(stored string) bool {
	if strings.TrimSpace(stored) == "" {
		return false
	}
	_, err := p.Decrypt(stored)
	return err == nil
}

func (p *PIIProtector) DeterministicToken(plaintext string) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return "", nil
	}
	version, keyMaterial, err := p.activeKey(context.Background())
	if err != nil {
		return "", err
	}
	return deterministicToken(version, keyMaterial, plaintext), nil
}

func (p *PIIProtector) DeterministicTokens(plaintext string) ([]string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return nil, nil
	}
	if p.DB == nil {
		if strings.TrimSpace(p.FallbackSeed) == "" {
			return nil, errors.New("no active encryption key")
		}
		return []string{deterministicToken(1, p.FallbackSeed, plaintext)}, nil
	}

	rows, err := p.DB.QueryContext(context.Background(), `
		SELECT key_version, key_value
		FROM encryption_keys
		WHERE key_name=$1
		ORDER BY key_version DESC
	`, p.KeyName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var version int
		var keyValue string
		if rows.Scan(&version, &keyValue) == nil {
			keyValue, err = p.decryptKeyMaterial(keyValue)
			if err != nil {
				return nil, err
			}
			out = append(out, deterministicToken(version, keyValue, plaintext))
		}
	}
	if len(out) == 0 && strings.TrimSpace(p.FallbackSeed) != "" {
		out = append(out, deterministicToken(1, p.FallbackSeed, plaintext))
	}
	if len(out) == 0 {
		return nil, errors.New("no key versions available")
	}
	return out, nil
}

func (p *PIIProtector) LegacyDeterministicTokens(plaintext string) ([]string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return nil, nil
	}
	if p.DB == nil {
		if strings.TrimSpace(p.FallbackSeed) == "" {
			return nil, errors.New("no active encryption key")
		}
		return []string{legacyDeterministicToken(p.FallbackSeed, plaintext)}, nil
	}

	rows, err := p.DB.QueryContext(context.Background(), `
		SELECT key_value
		FROM encryption_keys
		WHERE key_name=$1
		ORDER BY key_version DESC
	`, p.KeyName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var keyValue string
		if rows.Scan(&keyValue) == nil {
			keyValue, err = p.decryptKeyMaterial(keyValue)
			if err != nil {
				return nil, err
			}
			out = append(out, legacyDeterministicToken(keyValue, plaintext))
		}
	}
	if len(out) == 0 && strings.TrimSpace(p.FallbackSeed) != "" {
		out = append(out, legacyDeterministicToken(p.FallbackSeed, plaintext))
	}
	if len(out) == 0 {
		return nil, errors.New("no key versions available")
	}
	return out, nil
}

func deterministicToken(version int, keyMaterial, plaintext string) string {
	return fmt.Sprintf("tk%d:%s", version, legacyDeterministicToken(keyMaterial, plaintext))
}

func legacyDeterministicToken(keyMaterial, plaintext string) string {
	mac := hmac.New(sha256.New, []byte("deterministic:"+keyMaterial))
	_, _ = mac.Write([]byte(strings.TrimSpace(plaintext)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (p *PIIProtector) EnsureBootstrapKey() error {
	var exists bool
	err := p.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM encryption_keys
			WHERE key_name=$1 AND key_version=1
		)
	`, p.KeyName).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	sealed, err := NewSecretStoreProtector(strings.TrimSpace(os.Getenv("SECRET_MASTER_KEY"))).EncryptIfNeeded(p.FallbackSeed)
	if err != nil {
		return err
	}
	_, err = p.DB.Exec(`
		INSERT INTO encryption_keys(id, key_name, key_version, key_value, status, valid_from, created_at)
		VALUES ($1,$2,1,$3,'ACTIVE',now(),now())
	`, uuid.NewString(), p.KeyName, sealed)
	return err
}

func (p *PIIProtector) activeKey(ctx context.Context) (int, string, error) {
	if p.DB == nil {
		if strings.TrimSpace(p.FallbackSeed) == "" {
			return 0, "", errors.New("no active encryption key")
		}
		return 1, p.FallbackSeed, nil
	}
	var version int
	var keyValue string
	err := p.DB.QueryRowContext(ctx, `
		SELECT key_version, key_value
		FROM encryption_keys
		WHERE key_name=$1 AND status='ACTIVE' AND revoked_at IS NULL
		ORDER BY key_version DESC
		LIMIT 1
	`, p.KeyName).Scan(&version, &keyValue)
	if err == sql.ErrNoRows {
		if strings.TrimSpace(p.FallbackSeed) == "" {
			return 0, "", errors.New("no active encryption key")
		}
		return 1, p.FallbackSeed, nil
	}
	if err != nil {
		return 0, "", err
	}
	keyValue, err = p.decryptKeyMaterial(keyValue)
	if err != nil {
		return 0, "", err
	}
	return version, keyValue, nil
}

func (p *PIIProtector) keyByVersion(ctx context.Context, version int) (string, error) {
	if p.DB == nil {
		if strings.TrimSpace(p.FallbackSeed) == "" || version != 1 {
			return "", sql.ErrNoRows
		}
		return p.FallbackSeed, nil
	}
	var keyValue string
	err := p.DB.QueryRowContext(ctx, `
		SELECT key_value
		FROM encryption_keys
		WHERE key_name=$1 AND key_version=$2
		LIMIT 1
	`, p.KeyName, version).Scan(&keyValue)
	if err == sql.ErrNoRows && strings.TrimSpace(p.FallbackSeed) != "" && version == 1 {
		return p.FallbackSeed, nil
	}
	if err == nil {
		keyValue, err = p.decryptKeyMaterial(keyValue)
		if err != nil {
			return "", err
		}
	}
	return keyValue, err
}

func (p *PIIProtector) decryptKeyMaterial(value string) (string, error) {
	store := NewSecretStoreProtector(strings.TrimSpace(os.Getenv("SECRET_MASTER_KEY")))
	plain, err := store.DecryptIfNeeded(value)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(strings.TrimSpace(value), secretEnvelopePrefix) && strings.TrimSpace(plain) == "" {
		return "", errors.New("decrypted key material is empty")
	}
	if strings.TrimSpace(plain) == "" {
		return value, nil
	}
	return plain, nil
}

func (p *PIIProtector) VerifyActiveKeyMaterial() error {
	if p == nil {
		return errors.New("nil pii protector")
	}
	if _, _, err := p.activeKey(context.Background()); err != nil {
		return err
	}
	probe := "key-material-probe"
	enc, err := p.Encrypt(probe)
	if err != nil {
		return err
	}
	dec, err := p.Decrypt(enc)
	if err != nil {
		return err
	}
	if dec != probe {
		return errors.New("active key material verification failed")
	}
	return nil
}

func seal(keyMaterial, plaintext string) (string, error) {
	key := deriveKey(keyMaterial)
	block, err := aes.NewCipher(key)
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
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	combined := append(nonce, sealed...)
	return base64.RawStdEncoding.EncodeToString(combined), nil
}

func open(keyMaterial, encoded string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	key := deriveKey(keyMaterial)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	n := gcm.NonceSize()
	if len(raw) < n {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:n], raw[n:]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func deriveKey(seed string) []byte {
	h := sha256.Sum256([]byte(seed))
	return h[:]
}

func HashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func parseEncryptedEnvelope(stored string) (int, string, error) {
	parts := strings.SplitN(strings.TrimSpace(stored), ":", 2)
	if len(parts) != 2 || len(parts[0]) < 2 || parts[0][0] != 'k' {
		return 0, "", errors.New("invalid encrypted payload format")
	}
	for _, r := range parts[0][1:] {
		if r < '0' || r > '9' {
			return 0, "", errors.New("invalid encrypted payload version")
		}
	}
	version, err := strconv.Atoi(parts[0][1:])
	if err != nil {
		return 0, "", err
	}
	if strings.TrimSpace(parts[1]) == "" {
		return 0, "", errors.New("invalid encrypted payload body")
	}
	return version, parts[1], nil
}
