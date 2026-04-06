package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"meridian/backend/internal/platform/security"
	"meridian/backend/internal/service"
)

type SupportHandler struct {
	DB      *sql.DB
	Svc     *service.SupportService
	Secrets *security.PIIProtector
}

const maxAttachmentBytes = 10 * 1024 * 1024

func NewSupportHandler(db *sql.DB, secrets *security.PIIProtector) *SupportHandler {
	return &SupportHandler{DB: db, Svc: service.NewSupportService(db), Secrets: secrets}
}

type supportAccess struct {
	UserID         string
	SiteCode       string
	Global         bool
	AssignedOnly   bool
	HasSiteScope   bool
	HasAnyScopeRow bool
}

func (h *SupportHandler) loadAccess(c *gin.Context) (supportAccess, error) {
	access := supportAccess{UserID: c.GetString("userID")}
	if strings.TrimSpace(access.UserID) == "" {
		return access, errors.New("missing actor")
	}
	if err := h.DB.QueryRow(`SELECT COALESCE(site_code,'SITE-A') FROM users WHERE id=$1`, access.UserID).Scan(&access.SiteCode); err != nil {
		return access, err
	}

	rows, err := h.DB.Query(`
		SELECT scope
		FROM user_roles ur
		JOIN scope_rules sr ON sr.role_id=ur.role_id
		WHERE ur.user_id=$1 AND sr.module='support'
	`, access.UserID)
	if err != nil {
		return access, err
	}
	defer rows.Close()

	for rows.Next() {
		var scope string
		if rows.Scan(&scope) != nil {
			continue
		}
		access.HasAnyScopeRow = true
		s := strings.ToLower(strings.TrimSpace(scope))
		if s == "global" {
			access.Global = true
		}
		if s == "site" {
			access.HasSiteScope = true
		}
		if s == "assigned" {
			access.AssignedOnly = true
		}
	}

	if access.Global {
		access.AssignedOnly = false
	}
	if access.HasSiteScope {
		access.AssignedOnly = false
	}
	return access, nil
}

func (h *SupportHandler) hasAssignment(userID, entityType, entityID string) bool {
	var ok bool
	_ = h.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1
			FROM user_assignments
			WHERE user_id=$1::uuid AND entity_type=$2 AND entity_id=$3
		)
	`, userID, entityType, entityID).Scan(&ok)
	return ok
}

func (h *SupportHandler) ticketAllowed(access supportAccess, ticketID string) (bool, error) {
	if access.Global {
		return true, nil
	}

	var siteCode string
	var assignee sql.NullString
	var orderID string
	err := h.DB.QueryRow(`SELECT COALESCE(calendar_site_code,'SITE-A'), assignee_id::text, order_id FROM support_tickets WHERE id=$1`, ticketID).Scan(&siteCode, &assignee, &orderID)
	if err != nil {
		return false, err
	}

	if access.AssignedOnly {
		if assignee.Valid && strings.EqualFold(assignee.String, access.UserID) {
			return true, nil
		}
		return h.hasAssignment(access.UserID, "ticket", ticketID) || h.hasAssignment(access.UserID, "order", orderID), nil
	}
	return strings.EqualFold(siteCode, access.SiteCode), nil
}

func (h *SupportHandler) ListTickets(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	query := `
		SELECT id::text, order_id, ticket_type, priority, status, record_version,
		       COALESCE(sla_due_at, created_at), escalated, created_at
		FROM support_tickets
	`
	args := []any{}
	if !access.Global {
		if access.AssignedOnly {
			query += ` WHERE assignee_id=$1::uuid OR id::text IN (SELECT entity_id FROM user_assignments WHERE user_id=$1::uuid AND entity_type='ticket') `
			args = append(args, access.UserID)
		} else {
			query += ` WHERE COALESCE(calendar_site_code,'SITE-A')=$1 `
			args = append(args, access.SiteCode)
		}
	}
	query += `
		ORDER BY created_at DESC
		LIMIT 300
	`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tickets"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, orderID, ttype, priority, status string
		var version int
		var dueAt, createdAt time.Time
		var escalated bool
		if rows.Scan(&id, &orderID, &ttype, &priority, &status, &version, &dueAt, &escalated, &createdAt) == nil {
			out = append(out, gin.H{
				"id":             id,
				"order_id":       orderID,
				"ticket_type":    ttype,
				"priority":       priority,
				"status":         status,
				"record_version": version,
				"sla_due_at":     dueAt,
				"sla_seconds":    int(time.Until(dueAt).Seconds()),
				"escalated":      escalated,
				"created_at":     createdAt,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"tickets": out})
}

func (h *SupportHandler) ListOrders(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	query := `
		SELECT id, customer_ref, site_code, created_at
		FROM orders
	`
	args := []any{}
	if !access.Global {
		if access.AssignedOnly {
			query += ` WHERE id IN (SELECT entity_id FROM user_assignments WHERE user_id=$1::uuid AND entity_type='order') `
			args = append(args, access.UserID)
		} else {
			query += ` WHERE site_code=$1 `
			args = append(args, access.SiteCode)
		}
	}
	query += ` ORDER BY created_at DESC LIMIT 300 `

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load orders"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var id, customerRef, siteCode string
		var createdAt time.Time
		if rows.Scan(&id, &customerRef, &siteCode, &createdAt) == nil {
			out = append(out, gin.H{
				"id":           id,
				"customer_ref": customerRef,
				"site_code":    siteCode,
				"created_at":   createdAt,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": out})
}

func (h *SupportHandler) UpdateTicket(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	var req struct {
		Description string `json:"description"`
		Version     int    `json:"record_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	allowed, aerr := h.ticketAllowed(access, id)
	if aerr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ticket not found"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "ticket outside scope"})
		return
	}

	var current int
	err = h.DB.QueryRow(`SELECT record_version FROM support_tickets WHERE id=$1`, id).Scan(&current)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ticket not found"})
		return
	}
	if req.Version != current {
		c.JSON(http.StatusConflict, gin.H{"error": "version conflict", "current_version": current})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE support_tickets
		SET description=$2, record_version=record_version+1, updated_at=now()
		WHERE id=$1
	`, id, req.Description)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ticket update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true, "record_version": current + 1})
}

func (h *SupportHandler) CreateTicket(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	var req struct {
		OrderID      string `json:"order_id"`
		TicketType   string `json:"ticket_type"`
		Priority     string `json:"priority"`
		Description  string `json:"description"`
		Attachments  []any  `json:"attachments"`
		BusinessSite string `json:"business_site"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	tt := strings.ToLower(strings.TrimSpace(req.TicketType))
	if tt != "return_and_refund" && tt != "refund_only" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ticket_type must be return_and_refund or refund_only"})
		return
	}
	if len(req.Attachments) > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "maximum 5 attachments"})
		return
	}
	if req.BusinessSite == "" {
		req.BusinessSite = "SITE-A"
	}

	if !access.Global {
		req.BusinessSite = access.SiteCode
	}

	var orderSite string
	if err := h.DB.QueryRow(`SELECT site_code FROM orders WHERE id=$1`, req.OrderID).Scan(&orderSite); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order not found"})
		return
	}
	if !access.Global && !strings.EqualFold(orderSite, req.BusinessSite) {
		c.JSON(http.StatusForbidden, gin.H{"error": "order outside support scope"})
		return
	}

	elig, err := h.Svc.EvaluateEligibility(req.OrderID, tt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order is not eligible for the requested support flow"})
		return
	}
	if elig.RefundOnly && tt == "return_and_refund" {
		tt = "refund_only"
	}

	slaDue, err := h.Svc.ComputeSLADue(req.BusinessSite, req.Priority, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid business calendar"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start ticket transaction"})
		return
	}
	defer tx.Rollback()

	id := uuid.NewString()
	var assigneeID any
	if access.AssignedOnly {
		assigneeID = access.UserID
	}
	_, err = tx.Exec(`
		INSERT INTO support_tickets(id, order_id, ticket_type, priority, description, status, assignee_id, record_version, created_at, sla_due_at, calendar_site_code)
		VALUES ($1,$2,$3,$4,$5,'OPEN',$6::uuid,1,now(),$7,$8)
	`, id, req.OrderID, tt, strings.ToUpper(req.Priority), req.Description, assigneeID, slaDue, req.BusinessSite)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ticket creation failed"})
		return
	}

	if _, err = tx.Exec(`
		INSERT INTO user_assignments(user_id, entity_type, entity_id)
		VALUES ($1::uuid,'ticket',$2), ($1::uuid,'order',$3)
		ON CONFLICT (user_id, entity_type, entity_id) DO NOTHING
	`, access.UserID, id, req.OrderID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign support entities"})
		return
	}

	rows, err := tx.Query(`SELECT id::text, returnable FROM order_lines WHERE order_id=$1`, req.OrderID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var lineID string
			var returnable bool
			if rows.Scan(&lineID, &returnable) != nil {
				continue
			}
			action := "return_and_refund"
			if !returnable {
				action = "refund_only"
			}
			_, _ = tx.Exec(`
				INSERT INTO support_ticket_lines(ticket_id, order_line_id, requested_action)
				VALUES ($1,$2::uuid,$3)
			`, id, lineID, action)
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ticket commit failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":             id,
		"record_version": 1,
		"sla_due_at":     slaDue,
		"eligibility":    elig,
	})
}

func (h *SupportHandler) AddAttachment(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	ticketID := c.Param("id")
	var req struct {
		FileName    string `json:"file_name"`
		MimeType    string `json:"mime_type"`
		SizeMB      int    `json:"size_mb"`
		SizeBytes   int64  `json:"size_bytes"`
		Checksum    string `json:"checksum"`
		ContentBase string `json:"content_base64"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if req.SizeMB <= 0 || req.SizeMB > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment exceeds 10MB limit"})
		return
	}
	allowedTypes := map[string]bool{"image/jpeg": true, "image/png": true, "application/pdf": true}
	if !allowedTypes[strings.ToLower(req.MimeType)] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported attachment type"})
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.ContentBase)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment content is not valid base64"})
		return
	}
	if len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment content is required"})
		return
	}
	if int64(len(raw)) > maxAttachmentBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment exceeds 10MB limit"})
		return
	}
	if req.SizeBytes > 0 && req.SizeBytes != int64(len(raw)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment size mismatch"})
		return
	}
	if hash := sha256.Sum256(raw); !strings.EqualFold(req.Checksum, hex.EncodeToString(hash[:])) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment checksum mismatch"})
		return
	}

	var count int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM ticket_attachments WHERE ticket_id=$1`, ticketID).Scan(&count)
	if count >= 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ticket already has 5 attachments"})
		return
	}

	allowed, aerr := h.ticketAllowed(access, ticketID)
	if aerr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ticket not found"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "ticket outside scope"})
		return
	}

	var exists bool
	err = h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ticket_attachments WHERE ticket_id=$1 AND checksum=$2)`, ticketID, req.Checksum).Scan(&exists)
	if err == nil && exists {
		c.JSON(http.StatusConflict, gin.H{"error": "duplicate attachment checksum"})
		return
	}

	attachmentID := uuid.NewString()
	storagePath, err := h.persistSupportAttachment(ticketID, attachmentID, req.FileName, req.MimeType, raw)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "attachment storage failed"})
		return
	}
	sizeMB := req.SizeMB
	if sizeMB <= 0 {
		sizeMB = maxInt(1, int((len(raw)+(1024*1024)-1)/(1024*1024)))
	}
	_, err = h.DB.Exec(`
		INSERT INTO ticket_attachments(id, ticket_id, file_name, mime_type, size_mb, size_bytes, checksum, storage_path, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
	`, attachmentID, ticketID, req.FileName, strings.ToLower(req.MimeType), sizeMB, len(raw), req.Checksum, storagePath)
	if err != nil {
		_ = os.Remove(storagePath)
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment save failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"ok": true, "attachment_id": attachmentID})
}

func (h *SupportHandler) GetTicket(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	allowed, aerr := h.ticketAllowed(access, id)
	if aerr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ticket not found"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "ticket outside scope"})
		return
	}

	var orderID, ticketType, priority, status, desc, siteCode string
	var version int
	var dueAt time.Time
	var escalated bool
	err = h.DB.QueryRow(`
		SELECT order_id, ticket_type, priority, status, description, record_version, COALESCE(calendar_site_code,'SITE-A'), COALESCE(sla_due_at, now()), escalated
		FROM support_tickets WHERE id=$1
	`, id).Scan(&orderID, &ticketType, &priority, &status, &desc, &version, &siteCode, &dueAt, &escalated)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ticket not found"})
		return
	}

	timeLeft := time.Until(dueAt)
	c.JSON(http.StatusOK, gin.H{
		"id":             id,
		"order_id":       orderID,
		"ticket_type":    ticketType,
		"priority":       priority,
		"status":         status,
		"description":    desc,
		"record_version": version,
		"sla_due_at":     dueAt,
		"sla_seconds":    int(timeLeft.Seconds()),
		"site_code":      siteCode,
		"escalated":      escalated,
	})
}

func (h *SupportHandler) ResolveConflict(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	var req struct {
		CurrentVersion int    `json:"current_version"`
		Expected       int    `json:"expected_version"`
		Mode           string `json:"mode"`
		Description    string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	allowed, aerr := h.ticketAllowed(access, id)
	if aerr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ticket not found"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "ticket outside scope"})
		return
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode != "merge" && mode != "overwrite" && mode != "discard" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode must be merge|overwrite|discard"})
		return
	}

	if req.Expected != req.CurrentVersion {
		if mode == "discard" {
			c.JSON(http.StatusOK, gin.H{"discarded": true})
			return
		}
		if mode == "merge" {
			if _, err := h.DB.Exec(`
				UPDATE support_tickets
				SET description=description || E'\n' || $2, conflict_note='merged client change', record_version=record_version+1, updated_at=now()
				WHERE id=$1
			`, id, req.Description); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "conflict merge failed"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"merged": true})
			return
		}
	}

	if mode == "overwrite" {
		if _, err := h.DB.Exec(`
			UPDATE support_tickets
			SET description=$2, conflict_note='overwritten by client', record_version=record_version+1, updated_at=now()
			WHERE id=$1
		`, id, req.Description); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "conflict overwrite failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"overwritten": true})
		return
	}

	c.JSON(http.StatusOK, gin.H{"unchanged": true})
}

func (h *SupportHandler) ApproveRefund(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	var req struct {
		TicketID string `json:"ticket_id"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	allowed, aerr := h.ticketAllowed(access, req.TicketID)
	if aerr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ticket not found"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "ticket outside scope"})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE support_tickets
		SET status='REFUND_APPROVED', updated_at=now()
		WHERE id=$1
	`, req.TicketID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refund approval failed"})
		return
	}

	_, _ = h.DB.Exec(`
		INSERT INTO audit_logs(actor_id, action_class, entity_type, entity_id, event_data)
		VALUES ($1,'refund_approval','support_ticket',$2,$3::jsonb)
	`, c.GetString("userID"), req.TicketID, `{"note":"`+strings.ReplaceAll(req.Note, "\"", "")+`"}`)

	c.JSON(http.StatusOK, gin.H{"approved": true})
}

func (h *SupportHandler) persistSupportAttachment(ticketID, attachmentID, fileName, mimeType string, raw []byte) (string, error) {
	root := strings.TrimSpace(os.Getenv("ATTACHMENT_STORAGE_DIR"))
	if root == "" {
		root = filepath.Join(".", "data", "support_attachments")
	}
	dir := filepath.Join(root, ticketID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(fileName)))
	if ext == "" {
		switch strings.ToLower(strings.TrimSpace(mimeType)) {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "application/pdf":
			ext = ".pdf"
		default:
			ext = ".bin"
		}
	}
	path := filepath.Join(dir, attachmentID+ext)
	encrypted := raw
	if h.Secrets != nil {
		payload, err := h.Secrets.EncryptBytes(raw)
		if err != nil {
			return "", err
		}
		if len(payload) > 0 {
			encrypted = payload
		}
	}
	if err := os.WriteFile(path, encrypted, 0o640); err != nil {
		return "", err
	}
	return path, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
