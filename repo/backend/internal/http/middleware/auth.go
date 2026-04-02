package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"meridian/backend/internal/platform/security"
)

func RequireAccessToken(tokens *security.TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}

		claims, err := tokens.ParseToken(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		if claims["kind"] != "access" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token kind"})
			return
		}

		sub, _ := claims["sub"].(string)
		username, _ := claims["username"].(string)
		c.Set(CtxUserID, sub)
		c.Set(CtxUsername, username)
		c.Next()
	}
}

func RequireStepUp(tokens *security.TokenManager, actionClass string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tok := c.GetHeader("X-Step-Up-Token")
		if tok == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "missing step-up token"})
			return
		}
		claims, err := tokens.ParseToken(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid step-up token"})
			return
		}
		if claims["kind"] != "step_up" || claims["action_class"] != actionClass {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "step-up scope mismatch"})
			return
		}

		uid := c.GetString(CtxUserID)
		sub, _ := claims["sub"].(string)
		if uid == "" || uid != sub {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "step-up user mismatch"})
			return
		}
		c.Next()
	}
}
