package middleware

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func AuditWrites(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			return
		}
		actor := c.GetString(CtxUserID)
		if actor == "" {
			actor = "anonymous"
		}
		action := c.Request.Method + " " + c.FullPath()
		event := fmt.Sprintf(`{"status":%d,"ts":"%s"}`, c.Writer.Status(), time.Now().UTC().Format(time.RFC3339))
		_, _ = db.Exec(`
			INSERT INTO audit_logs(actor_id, action_class, entity_type, entity_id, event_data)
			VALUES ($1,$2,'http_route',$3,$4::jsonb)
		`, actor, action, c.FullPath(), event)
	}
}
