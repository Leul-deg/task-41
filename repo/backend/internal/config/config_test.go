package config

import "testing"

func TestValidateSecurity_AllowsDevDefaults(t *testing.T) {
	c := Config{
		AppEnv:             "dev",
		JWTSecret:          "change-this-jwt-secret",
		SecretMasterKey:    "change-this-secret-master-key",
		BootstrapClientSec: "local-h5-secret-change-me",
		DefaultAdminPass:   "LocalAdminPass123!",
		PIIKeyValue:        "change-this-local-pii-key",
		KioskSubmitSecret:  "local-kiosk-submit-secret-change-me",
	}
	if err := c.ValidateSecurity(); err != nil {
		t.Fatalf("expected dev defaults allowed, got %v", err)
	}
}

func TestValidateSecurity_RejectsProdDefaults(t *testing.T) {
	c := Config{
		AppEnv:             "prod",
		JWTSecret:          "change-this-jwt-secret",
		SecretMasterKey:    "change-this-secret-master-key",
		BootstrapClientSec: "local-h5-secret-change-me",
		DefaultAdminPass:   "LocalAdminPass123!",
		PIIKeyValue:        "change-this-local-pii-key",
		KioskSubmitSecret:  "local-kiosk-submit-secret-change-me",
	}
	if err := c.ValidateSecurity(); err == nil {
		t.Fatal("expected security validation failure for production defaults")
	}
}
