package handlers

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"meridian/backend/internal/platform/masking"
	"meridian/backend/internal/service"
)

type ComplianceHandler struct {
	DB  *sql.DB
	Svc *service.ComplianceService
}

func NewComplianceHandler(db *sql.DB) *ComplianceHandler {
	return &ComplianceHandler{DB: db, Svc: service.NewComplianceService(db)}
}

func (h *ComplianceHandler) RunCrawler(c *gin.Context) {
	indexed, queued, err := h.Svc.RunCrawler()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "crawler run failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"indexed": indexed, "queued": queued})
}

func (h *ComplianceHandler) CrawlerStatus(c *gin.Context) {
	var sources, pending int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM crawler_sources WHERE approved=true`).Scan(&sources)
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM crawler_queue WHERE status='PENDING'`).Scan(&pending)
	c.JSON(http.StatusOK, gin.H{"sources": sources, "pending_queue": pending})
}

func (h *ComplianceHandler) CreateDeletionRequest(c *gin.Context) {
	var req struct {
		SubjectRef string `json:"subject_ref"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.SubjectRef == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subject_ref required"})
		return
	}
	var id string
	err := h.DB.QueryRow(`
		INSERT INTO deletion_requests(subject_ref, due_at)
		VALUES ($1, now() + interval '30 days')
		RETURNING id::text
	`, req.SubjectRef).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create deletion request"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *ComplianceHandler) ProcessDeletionRequest(c *gin.Context) {
	id := c.Param("id")
	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start deletion transaction"})
		return
	}
	defer tx.Rollback()

	var policy string
	var subjectRef string
	var status string
	err = tx.QueryRow(`
		SELECT COALESCE(policy_result,''), subject_ref, status
		FROM deletion_requests
		WHERE id=$1::uuid
		FOR UPDATE
	`, id).Scan(&policy, &subjectRef, &status)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deletion request not found"})
		return
	}
	if strings.EqualFold(status, "COMPLETED") {
		c.JSON(http.StatusConflict, gin.H{"error": "deletion request already completed"})
		return
	}

	if policy == "" {
		policy = "anonymize"
	}

	var result sql.Result
	if policy == "hard_delete" {
		result, err = tx.Exec(`DELETE FROM candidates WHERE id=$1::uuid`, subjectRef)
	} else {
		result, err = tx.Exec(`UPDATE candidates SET full_name='ANONYMIZED', email=NULL, phone=NULL, ssn_raw=NULL WHERE id=$1::uuid`, subjectRef)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deletion mutation failed"})
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify deletion mutation"})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deletion subject not found"})
		return
	}
	if _, err = tx.Exec(`UPDATE deletion_requests SET status='COMPLETED' WHERE id=$1::uuid`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to finalize deletion request"})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit deletion request"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"processed": true, "policy": policy})
}

func (h *ComplianceHandler) ListDeletionRequests(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT id::text, subject_ref, requested_at, due_at, status, COALESCE(policy_result,'')
		FROM deletion_requests
		ORDER BY requested_at DESC
		LIMIT 200
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load deletion requests"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, subjectRef, status, policy string
		var requestedAt, dueAt time.Time
		if rows.Scan(&id, &subjectRef, &requestedAt, &dueAt, &status, &policy) == nil {
			out = append(out, gin.H{"id": id, "subject_ref": subjectRef, "requested_at": requestedAt, "due_at": dueAt, "status": status, "policy_result": policy})
		}
	}
	c.JSON(http.StatusOK, gin.H{"requests": out})
}

func (h *ComplianceHandler) RetentionStatus(c *gin.Context) {
	var rejectedToPurge int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM candidate_rejections WHERE retention_until <= now()`).Scan(&rejectedToPurge)
	var supportToAnonymize int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM support_tickets WHERE created_at <= now() - interval '7 years' AND description <> '[ANONYMIZED BY RETENTION JOB]'`).Scan(&supportToAnonymize)
	var financialToAnonymize int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM orders WHERE created_at <= now() - interval '7 years' AND customer_ref <> 'ANONYMIZED_FINANCIAL_RECORD'`).Scan(&financialToAnonymize)
	c.JSON(http.StatusOK, gin.H{
		"rejected_candidates_due": rejectedToPurge,
		"support_records_due":     supportToAnonymize,
		"financial_records_due":   financialToAnonymize,
		"checked_at":              time.Now().UTC(),
	})
}

func (h *ComplianceHandler) AuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit
	action := strings.TrimSpace(c.Query("action"))

	query := `
		SELECT actor_id, action_class, entity_type, entity_id, created_at
		FROM audit_logs
	`
	args := []any{}
	if action != "" {
		query += ` WHERE action_class ILIKE $1 `
		args = append(args, "%"+action+"%")
	}
	if len(args) == 0 {
		query += ` ORDER BY created_at DESC LIMIT $1 OFFSET $2 `
		args = append(args, limit, offset)
	} else {
		query += ` ORDER BY created_at DESC LIMIT $2 OFFSET $3 `
		args = append(args, limit, offset)
	}
	rows, err := h.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load audit logs"})
		return
	}
	defer rows.Close()

	logs := []gin.H{}
	for rows.Next() {
		var actor, action, et, eid string
		var at time.Time
		if rows.Scan(&actor, &action, &et, &eid, &at) == nil {
			logs = append(logs, gin.H{"actor": actor, "action": action, "entity_type": et, "entity_id": eid, "at": at})
		}
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs, "page": page, "limit": limit, "action_filter": action})
}

func (h *ComplianceHandler) ExportAuditLogs(c *gin.Context) {
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "csv")))
	limitStr := c.DefaultQuery("limit", "500")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 5000 {
		limit = 500
	}

	rows, err := h.DB.Query(`
		SELECT actor_id, action_class, entity_type, entity_id, event_data::text, created_at
		FROM audit_logs
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to export audit logs"})
		return
	}
	defer rows.Close()

	records := []map[string]string{}
	for rows.Next() {
		var actor, action, entityType, entityID, eventData string
		var at time.Time
		if err := rows.Scan(&actor, &action, &entityType, &entityID, &eventData, &at); err == nil {
			records = append(records, map[string]string{
				"actor":       actor,
				"action":      action,
				"entity_type": entityType,
				"entity_id":   entityID,
				"event_data":  masking.MaskSSN(eventData),
				"created_at":  at.Format(time.RFC3339),
			})
		}
	}

	if format == "json" {
		c.Header("Content-Disposition", "attachment; filename=audit_logs.json")
		c.Header("Content-Type", "application/json")
		_ = json.NewEncoder(c.Writer).Encode(gin.H{"records": records})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=audit_logs.csv")
	c.Header("Content-Type", "text/csv")
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"actor", "action", "entity_type", "entity_id", "event_data", "created_at"})
	for _, r := range records {
		_ = w.Write([]string{r["actor"], r["action"], r["entity_type"], r["entity_id"], r["event_data"], r["created_at"]})
	}
	w.Flush()
}
