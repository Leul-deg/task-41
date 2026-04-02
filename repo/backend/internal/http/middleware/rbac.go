package middleware

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RequirePermission(db *sql.DB, module, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID := c.GetString(CtxUserID)
		if actorID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing actor"})
			return
		}

		var allowed bool
		err := db.QueryRow(`
			SELECT EXISTS(
				SELECT 1
				FROM user_roles ur
				JOIN role_permissions rp ON rp.role_id = ur.role_id
				JOIN permissions p ON p.id = rp.permission_id
				WHERE ur.user_id = $1 AND p.module = $2 AND p.action = $3
			)
		`, actorID, module, action).Scan(&allowed)
		if err != nil || !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}
		c.Next()
	}
}
