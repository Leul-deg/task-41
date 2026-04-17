package middleware

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"meridian/backend/internal/platform/security"
)

func RequireSignedRequests(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/healthz" {
			c.Next()
			return
		}

		clientKey := c.GetHeader("X-Client-Key")
		timestamp := c.GetHeader("X-Timestamp")
		nonce := c.GetHeader("X-Nonce")
		signature := c.GetHeader("X-Signature")

		if clientKey == "" || timestamp == "" || nonce == "" || signature == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing signing headers"})
			return
		}

		ts, err := time.Parse(time.RFC3339, timestamp)
		if err != nil || time.Since(ts) > 2*time.Minute || time.Until(ts) > 2*time.Minute {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "timestamp outside acceptance window"})
			return
		}

		var secret string
		err = db.QueryRow(`SELECT secret FROM client_keys WHERE key_id=$1 AND revoked_at IS NULL`, clientKey).Scan(&secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unknown client key"})
			return
		}
		secret, err = security.NewSecretStoreProtector(strings.TrimSpace(os.Getenv("SECRET_MASTER_KEY"))).DecryptIfNeeded(secret)
		if err != nil || strings.TrimSpace(secret) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid client secret material"})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "failed to read payload"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		payloadHash := security.PayloadHash(body)
		expected := security.ComputeSignature(secret, c.Request.Method, c.Request.URL.Path, timestamp, nonce, payloadHash)
		if !security.SignatureValid(expected, signature) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}

		if _, err := db.Exec(`INSERT INTO request_nonces(client_key, nonce, seen_at) VALUES ($1,$2,now())`, clientKey, nonce); err != nil {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "nonce already used"})
			return
		}

		c.Set("payloadHash", payloadHash)
		c.Next()
	}
}
