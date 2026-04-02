package middleware

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RequireScope(db *sql.DB, module string, allowed ...string) gin.HandlerFunc {
	allowedSet := map[string]bool{}
	for _, a := range allowed {
		allowedSet[a] = true
	}

	return func(c *gin.Context) {
		actor := c.GetString(CtxUserID)
		if actor == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing actor"})
			return
		}

		rows, err := db.Query(`
			SELECT sr.scope
			FROM user_roles ur
			JOIN scope_rules sr ON sr.role_id = ur.role_id
			WHERE ur.user_id=$1 AND sr.module=$2
		`, actor, module)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "scope query failed"})
			return
		}
		defer rows.Close()

		ok := false
		for rows.Next() {
			var scope string
			if rows.Scan(&scope) == nil && allowedSet[scope] {
				ok = true
				break
			}
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "scope denied"})
			return
		}

		c.Next()
	}
}
