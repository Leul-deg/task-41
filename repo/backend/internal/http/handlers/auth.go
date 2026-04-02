package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"meridian/backend/internal/platform/security"
)

type AuthHandler struct {
	DB         *sql.DB
	Tokens     *security.TokenManager
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	StepUpTTL  time.Duration
}

func NewAuthHandler(db *sql.DB, tokens *security.TokenManager, accessTTL, refreshTTL, stepUpTTL time.Duration) *AuthHandler {
	return &AuthHandler{DB: db, Tokens: tokens, AccessTTL: accessTTL, RefreshTTL: refreshTTL, StepUpTTL: stepUpTTL}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if len(req.Password) < 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 12 characters"})
		return
	}

	var userID, username, passwordHash string
	var failedAttempts int
	var lockedUntil sql.NullTime
	err := h.DB.QueryRow(`
		SELECT id, username, password_hash, failed_attempts, locked_until
		FROM users
		WHERE username=$1
	`, req.Username).Scan(&userID, &username, &passwordHash, &failedAttempts, &lockedUntil)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if lockedUntil.Valid && lockedUntil.Time.After(time.Now()) {
		c.JSON(http.StatusLocked, gin.H{"error": "account locked"})
		return
	}

	if !security.CheckPassword(passwordHash, req.Password) {
		failedAttempts++
		if failedAttempts >= 5 {
			_, _ = h.DB.Exec(`UPDATE users SET failed_attempts=0, locked_until=now()+interval '15 minutes' WHERE id=$1`, userID)
		} else {
			_, _ = h.DB.Exec(`UPDATE users SET failed_attempts=$1 WHERE id=$2`, failedAttempts, userID)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	_, _ = h.DB.Exec(`UPDATE users SET failed_attempts=0, locked_until=NULL, last_login_at=now() WHERE id=$1`, userID)

	access, err := h.Tokens.CreateAccessToken(userID, username, h.AccessTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token creation failed"})
		return
	}
	refresh, err := security.NewOpaqueToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "refresh creation failed"})
		return
	}
	refreshTTLSeconds := int64(h.RefreshTTL.Seconds())
	_, _ = h.DB.Exec(`
		INSERT INTO refresh_tokens(id, user_id, token, expires_at)
		VALUES ($1,$2,$3,now() + make_interval(secs => $4))
	`, uuid.NewString(), userID, refresh, refreshTTLSeconds)

	c.JSON(http.StatusOK, gin.H{"access_token": access, "refresh_token": refresh})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	var userID, username string
	err := h.DB.QueryRow(`
		SELECT u.id, u.username
		FROM refresh_tokens rt
		JOIN users u ON u.id = rt.user_id
		WHERE rt.token=$1 AND rt.revoked_at IS NULL AND rt.expires_at > now()
	`, req.RefreshToken).Scan(&userID, &username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}

	access, err := h.Tokens.CreateAccessToken(userID, username, h.AccessTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token creation failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"access_token": access})
}

type stepUpRequest struct {
	Password    string `json:"password"`
	ActionClass string `json:"action_class"`
}

func (h *AuthHandler) StepUp(c *gin.Context) {
	var req stepUpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing actor"})
		return
	}

	var passwordHash string
	err := h.DB.QueryRow(`SELECT password_hash FROM users WHERE id=$1`, userID).Scan(&passwordHash)
	if err != nil || !security.CheckPassword(passwordHash, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := h.Tokens.CreateStepUpToken(userID, req.ActionClass, h.StepUpTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue step-up token"})
		return
	}

	stepTTLSeconds := int64(h.StepUpTTL.Seconds())
	_, _ = h.DB.Exec(`
		INSERT INTO step_up_tokens(id, user_id, action_class, token, expires_at)
		VALUES ($1,$2,$3,$4,now() + make_interval(secs => $5))
	`, uuid.NewString(), userID, req.ActionClass, token, stepTTLSeconds)

	c.JSON(http.StatusOK, gin.H{"step_up_token": token})
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing actor"})
		return
	}

	var username string
	err := h.DB.QueryRow(`SELECT username FROM users WHERE id=$1`, userID).Scan(&username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unknown user"})
		return
	}

	roleRows, err := h.DB.Query(`
		SELECT r.code
		FROM user_roles ur
		JOIN roles r ON r.id=ur.role_id
		WHERE ur.user_id=$1
		ORDER BY r.code
	`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load roles"})
		return
	}
	defer roleRows.Close()

	roles := []string{}
	for roleRows.Next() {
		var code string
		if roleRows.Scan(&code) == nil {
			roles = append(roles, code)
		}
	}

	permRows, err := h.DB.Query(`
		SELECT p.module, p.action
		FROM user_roles ur
		JOIN role_permissions rp ON rp.role_id=ur.role_id
		JOIN permissions p ON p.id=rp.permission_id
		WHERE ur.user_id=$1
	`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load permissions"})
		return
	}
	defer permRows.Close()

	permissions := map[string]map[string]bool{}
	for permRows.Next() {
		var module, action string
		if permRows.Scan(&module, &action) == nil {
			if _, ok := permissions[module]; !ok {
				permissions[module] = map[string]bool{}
			}
			permissions[module][action] = true
		}
	}

	scopeRows, err := h.DB.Query(`
		SELECT sr.module, sr.scope, COALESCE(sr.scope_value,'')
		FROM user_roles ur
		JOIN scope_rules sr ON sr.role_id=ur.role_id
		WHERE ur.user_id=$1
	`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load scopes"})
		return
	}
	defer scopeRows.Close()

	scopes := map[string][]gin.H{}
	for scopeRows.Next() {
		var module, scope, value string
		if scopeRows.Scan(&module, &scope, &value) == nil {
			scopes[module] = append(scopes[module], gin.H{"scope": scope, "value": value})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          userID,
		"username":    username,
		"roles":       roles,
		"permissions": permissions,
		"scopes":      scopes,
	})
}
