package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv             string
	Port               string
	DatabaseURL        string
	JWTSecret          string
	AccessTTL          time.Duration
	RefreshTTL         time.Duration
	StepUpTTL          time.Duration
	DefaultAdminUser   string
	DefaultAdminPass   string
	BootstrapClientKey string
	BootstrapClientSec string
	IdempotencyTTL     time.Duration
	EnableFuzzyDedup   bool
	PIIKeyName         string
	PIIKeyValue        string
	KioskSubmitSecret  string
}

func Load() Config {
	return Config{
		AppEnv:             strings.ToLower(getEnv("APP_ENV", "dev")),
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://meridian:meridian@localhost:5432/meridian?sslmode=disable"),
		JWTSecret:          getEnv("JWT_SECRET", "change-this-jwt-secret"),
		AccessTTL:          time.Duration(getEnvInt("ACCESS_TOKEN_TTL_MIN", 15)) * time.Minute,
		RefreshTTL:         time.Duration(getEnvInt("REFRESH_TOKEN_TTL_HOURS", 168)) * time.Hour,
		StepUpTTL:          time.Duration(getEnvInt("STEP_UP_TTL_MIN", 5)) * time.Minute,
		DefaultAdminUser:   getEnv("DEFAULT_ADMIN_USERNAME", "admin"),
		DefaultAdminPass:   getEnv("DEFAULT_ADMIN_PASSWORD", "LocalAdminPass123!"),
		BootstrapClientKey: getEnv("BOOTSTRAP_CLIENT_KEY", "local-h5"),
		BootstrapClientSec: getEnv("BOOTSTRAP_CLIENT_SECRET", "local-h5-secret-change-me"),
		IdempotencyTTL:     time.Duration(getEnvInt("IDEMPOTENCY_TTL_HOURS", 48)) * time.Hour,
		EnableFuzzyDedup:   getEnvBool("ENABLE_FUZZY_DEDUP", true),
		PIIKeyName:         getEnv("PII_KEY_NAME", "PII_DEFAULT"),
		PIIKeyValue:        getEnv("PII_KEY_VALUE", "change-this-local-pii-key"),
		KioskSubmitSecret:  getEnv("KIOSK_SUBMIT_SECRET", "local-kiosk-submit-secret-change-me"),
	}
}

func (c Config) ValidateSecurity() error {
	if strings.TrimSpace(c.JWTSecret) == "" {
		return errors.New("JWT_SECRET is required")
	}
	if strings.TrimSpace(c.BootstrapClientSec) == "" {
		return errors.New("BOOTSTRAP_CLIENT_SECRET is required")
	}
	if strings.TrimSpace(c.PIIKeyValue) == "" {
		return errors.New("PII_KEY_VALUE is required")
	}
	if strings.TrimSpace(c.KioskSubmitSecret) == "" {
		return errors.New("KIOSK_SUBMIT_SECRET is required")
	}

	isDevLike := c.AppEnv == "" || c.AppEnv == "dev" || c.AppEnv == "local" || c.AppEnv == "test"
	if isDevLike {
		return nil
	}

	if c.JWTSecret == "change-this-jwt-secret" {
		return fmt.Errorf("JWT_SECRET must be rotated for %s", c.AppEnv)
	}
	if c.BootstrapClientSec == "local-h5-secret-change-me" {
		return fmt.Errorf("BOOTSTRAP_CLIENT_SECRET must be rotated for %s", c.AppEnv)
	}
	if c.DefaultAdminPass == "LocalAdminPass123!" {
		return fmt.Errorf("DEFAULT_ADMIN_PASSWORD must be rotated for %s", c.AppEnv)
	}
	if c.PIIKeyValue == "change-this-local-pii-key" {
		return fmt.Errorf("PII_KEY_VALUE must be rotated for %s", c.AppEnv)
	}
	if c.KioskSubmitSecret == "local-kiosk-submit-secret-change-me" {
		return fmt.Errorf("KIOSK_SUBMIT_SECRET must be rotated for %s", c.AppEnv)
	}
	return nil
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
