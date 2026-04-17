package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"meridian/backend/internal/platform/security"
)

type AdminHandler struct {
	DB          *sql.DB
	SecretStore *security.SecretStoreProtector
}

func NewAdminHandler(db *sql.DB, secretStore *security.SecretStoreProtector) *AdminHandler {
	return &AdminHandler{DB: db, SecretStore: secretStore}
}

func (h *AdminHandler) ListRoles(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id::text, code, name FROM roles ORDER BY code`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load roles"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, code, name string
		if rows.Scan(&id, &code, &name) == nil {
			out = append(out, gin.H{"id": id, "code": code, "name": name})
		}
	}
	c.JSON(http.StatusOK, gin.H{"roles": out})
}

func (h *AdminHandler) UpdateRolePermissions(c *gin.Context) {
	roleID := c.Param("id")
	var req struct {
		Permissions []struct {
			Module string `json:"module"`
			Action string `json:"action"`
		} `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM role_permissions WHERE role_id=$1::uuid`, roleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset role permissions"})
		return
	}
	for _, p := range req.Permissions {
		var permID string
		err := tx.QueryRow(`SELECT id::text FROM permissions WHERE module=$1 AND action=$2`, strings.ToLower(p.Module), strings.ToLower(p.Action)).Scan(&permID)
		if err != nil {
			if err != sql.ErrNoRows {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve permission"})
				return
			}
			permID = uuid.NewString()
			if _, err := tx.Exec(`INSERT INTO permissions(id, module, action) VALUES ($1,$2,$3) ON CONFLICT (module, action) DO NOTHING`, permID, strings.ToLower(p.Module), strings.ToLower(p.Action)); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create permission"})
				return
			}
			if err := tx.QueryRow(`SELECT id::text FROM permissions WHERE module=$1 AND action=$2`, strings.ToLower(p.Module), strings.ToLower(p.Action)).Scan(&permID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load permission"})
				return
			}
		}
		if _, err := tx.Exec(`INSERT INTO role_permissions(id, role_id, permission_id) VALUES ($1,$2::uuid,$3::uuid) ON CONFLICT DO NOTHING`, uuid.NewString(), roleID, permID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign permission"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

func (h *AdminHandler) UpdateRoleScopes(c *gin.Context) {
	roleID := c.Param("id")
	var req struct {
		Scopes []struct {
			Module string `json:"module"`
			Scope  string `json:"scope"`
			Value  string `json:"value"`
		} `json:"scopes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM scope_rules WHERE role_id=$1::uuid`, roleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset role scopes"})
		return
	}
	for _, s := range req.Scopes {
		if _, err := tx.Exec(`INSERT INTO scope_rules(id, role_id, module, scope, scope_value) VALUES ($1,$2::uuid,$3,$4,$5)`, uuid.NewString(), roleID, strings.ToLower(s.Module), strings.ToLower(s.Scope), s.Value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign scope"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

func (h *AdminHandler) RotateClientKey(c *gin.Context) {
	var req struct {
		KeyName string `json:"key_name"`
		Secret  string `json:"secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	seed := strings.ReplaceAll(uuid.NewString(), "-", "")
	if len(seed) > 8 {
		seed = seed[:8]
	}
	newKeyID := req.KeyName + "-v" + seed
	sealed := req.Secret
	if h.SecretStore != nil {
		var serr error
		sealed, serr = h.SecretStore.EncryptIfNeeded(req.Secret)
		if serr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to secure key secret"})
			return
		}
	}
	_, err := h.DB.Exec(`INSERT INTO client_keys(id, key_id, secret, created_at) VALUES ($1,$2,$3,now())`, uuid.NewString(), newKeyID, sealed)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to rotate key"})
		return
	}
	if _, err := h.DB.Exec(`
		INSERT INTO key_rotation_events(key_name, to_version, actor_id, event_type)
		VALUES ($1,1,$2::uuid,'ROTATE')
	`, req.KeyName, c.GetString("userID")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record key rotation"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"key_id": newKeyID})
}

func (h *AdminHandler) RevokeClientKey(c *gin.Context) {
	keyID := c.Param("id")
	_, err := h.DB.Exec(`UPDATE client_keys SET revoked_at=now() WHERE key_id=$1`, keyID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to revoke key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}
