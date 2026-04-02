package middleware

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type bodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func RequireIdempotency(db *sql.DB, ttl time.Duration) gin.HandlerFunc {
	protected := map[string]bool{
		"/api/inventory/reservations/order-create": true,
		"/api/support/tickets/refund-approve":        true,
	}

	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		if !protected[c.FullPath()] {
			c.Next()
			return
		}

		key := c.GetHeader("Idempotency-Key")
		if key == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing Idempotency-Key"})
			return
		}

		actorID := c.GetString(CtxUserID)
		payloadHash := c.GetString("payloadHash")
		endpoint := c.FullPath()

		var status int
		var response string
		err := db.QueryRow(`
			SELECT status_code, response_body
			FROM idempotency_records
			WHERE endpoint=$1 AND actor_id=$2 AND payload_hash=$3 AND idem_key=$4 AND expires_at > now()
		`, endpoint, actorID, payloadHash, key).Scan(&status, &response)
		if err == nil {
			c.Data(status, "application/json", []byte(response))
			c.Abort()
			return
		}

		writer := &bodyWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = writer
		c.Next()

		if c.Writer.Status() >= 200 && c.Writer.Status() < 500 {
			payload := writer.body.String()
			if !json.Valid([]byte(payload)) {
				payload = `{"message":"stored non-json response"}`
			}
			ttlSeconds := int64(ttl.Seconds())
			_, _ = db.Exec(`
				INSERT INTO idempotency_records(endpoint, actor_id, payload_hash, idem_key, status_code, response_body, expires_at)
				VALUES ($1,$2,$3,$4,$5,$6,now() + make_interval(secs => $7))
			`, endpoint, actorID, payloadHash, key, c.Writer.Status(), payload, ttlSeconds)
		}
	}
}
