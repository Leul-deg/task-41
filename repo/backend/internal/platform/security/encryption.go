package security

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
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

func (p *PIIProtector) Decrypt(stored string) (string, error) {
	if strings.TrimSpace(stored) == "" {
		return "", nil
	}
	parts := strings.SplitN(stored, ":", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "k") {
		return "", errors.New("invalid encrypted payload format")
	}
	v, err := strconv.Atoi(strings.TrimPrefix(parts[0], "k"))
	if err != nil {
		return "", err
	}
	keyMaterial, err := p.keyByVersion(context.Background(), v)
	if err != nil {
		return "", err
	}
	return open(keyMaterial, parts[1])
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
	_, err = p.DB.Exec(`
		INSERT INTO encryption_keys(id, key_name, key_version, key_value, status, valid_from, created_at)
		VALUES ($1,$2,1,$3,'ACTIVE',now(),now())
	`, uuid.NewString(), p.KeyName, p.FallbackSeed)
	return err
}

func (p *PIIProtector) activeKey(ctx context.Context) (int, string, error) {
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
	return version, keyValue, nil
}

func (p *PIIProtector) keyByVersion(ctx context.Context, version int) (string, error) {
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
	return keyValue, err
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
