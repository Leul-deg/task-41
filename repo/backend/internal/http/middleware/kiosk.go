package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func RequireKioskToken(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := strings.TrimSpace(c.GetHeader("X-Kiosk-Token"))
		if provided == "" || strings.TrimSpace(secret) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing kiosk token"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid kiosk token"})
			return
		}
		c.Next()
	}
}
