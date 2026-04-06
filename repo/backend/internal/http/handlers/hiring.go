package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"meridian/backend/internal/domain/hiring"
	"meridian/backend/internal/platform/masking"
	"meridian/backend/internal/platform/security"
	"meridian/backend/internal/service"
)

type HiringHandler struct {
	DB  *sql.DB
	Svc *service.HiringService
	PII *security.PIIProtector
}

func NewHiringHandler(db *sql.DB, enableFuzzy bool, pii *security.PIIProtector) *HiringHandler {
	return &HiringHandler{DB: db, Svc: service.NewHiringService(db, enableFuzzy, pii), PII: pii}
}

type hiringAccess struct {
	UserID       string
	SiteCode     string
	Global       bool
	AssignedOnly bool
	HasSiteScope bool
}

func (h *HiringHandler) loadAccess(c *gin.Context) (hiringAccess, error) {
	access := hiringAccess{UserID: c.GetString("userID")}
	if strings.TrimSpace(access.UserID) == "" {
		return access, fmt.Errorf("missing actor")
	}

	if err := h.DB.QueryRow(`SELECT COALESCE(site_code,'SITE-A') FROM users WHERE id=$1::uuid`, access.UserID).Scan(&access.SiteCode); err != nil {
		return access, err
	}

	rows, err := h.DB.Query(`
		SELECT scope
		FROM user_roles ur
		JOIN scope_rules sr ON sr.role_id=ur.role_id
		WHERE ur.user_id=$1::uuid AND sr.module='hiring'
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

	if access.Global || access.HasSiteScope {
		access.AssignedOnly = false
	}
	return access, nil
}

func (h *HiringHandler) hasAssignment(userID, entityType, entityID string) bool {
	var ok bool
	_ = h.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM user_assignments
			WHERE user_id=$1::uuid AND entity_type=$2 AND entity_id=$3
		)
	`, userID, entityType, entityID).Scan(&ok)
	return ok
}

func (h *HiringHandler) applicationExists(appID string) bool {
	var ok bool
	_ = h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM applications WHERE id=$1::uuid)`, appID).Scan(&ok)
	return ok
}

func (h *HiringHandler) candidateExists(candidateID string) bool {
	var ok bool
	_ = h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM candidates WHERE id=$1::uuid)`, candidateID).Scan(&ok)
	return ok
}

func (h *HiringHandler) canAccessJob(access hiringAccess, jobID string) (bool, error) {
	if access.Global {
		return true, nil
	}
	if access.AssignedOnly {
		return h.hasAssignment(access.UserID, "job", jobID), nil
	}
	var siteCode string
	if err := h.DB.QueryRow(`SELECT COALESCE(site_code,'') FROM job_postings WHERE id=$1::uuid`, jobID).Scan(&siteCode); err != nil {
		return false, err
	}
	return strings.EqualFold(siteCode, access.SiteCode), nil
}

func (h *HiringHandler) canAccessApplication(access hiringAccess, appID string) (bool, error) {
	if access.Global {
		return true, nil
	}
	if access.AssignedOnly {
		return h.hasAssignment(access.UserID, "application", appID), nil
	}
	var siteCode string
	err := h.DB.QueryRow(`
		SELECT COALESCE(j.site_code,'')
		FROM applications a
		JOIN job_postings j ON j.id=a.job_id
		WHERE a.id=$1::uuid
	`, appID).Scan(&siteCode)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(siteCode, access.SiteCode), nil
}

func (h *HiringHandler) canAccessCandidate(access hiringAccess, candidateID string) (bool, error) {
	if access.Global {
		return true, nil
	}
	if access.AssignedOnly {
		if h.hasAssignment(access.UserID, "candidate", candidateID) {
			return true, nil
		}
		var appID string
		err := h.DB.QueryRow(`
			SELECT a.id::text
			FROM applications a
			WHERE a.candidate_id=$1::uuid
			ORDER BY a.created_at DESC
			LIMIT 1
		`, candidateID).Scan(&appID)
		if err == nil && appID != "" {
			return h.hasAssignment(access.UserID, "application", appID), nil
		}
		return false, nil
	}

	var count int
	err := h.DB.QueryRow(`
		SELECT COUNT(*)
		FROM applications a
		JOIN job_postings j ON j.id=a.job_id
		WHERE a.candidate_id=$1::uuid AND COALESCE(j.site_code,'')=$2
	`, candidateID, access.SiteCode).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *HiringHandler) CreateJob(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed", "code": "FORBIDDEN_SCOPE"})
		return
	}

	var req struct {
		Code        string `json:"code"`
		Title       string `json:"title"`
		Description string `json:"description"`
		SiteCode    string `json:"site_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if !access.Global {
		req.SiteCode = access.SiteCode
	}

	id := uuid.NewString()
	_, err = h.DB.Exec(`
		INSERT INTO job_postings(id, code, title, description, site_code, created_at)
		VALUES ($1,$2,$3,$4,$5,now())
	`, id, req.Code, req.Title, req.Description, req.SiteCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job create failed"})
		return
	}
	if _, err = h.DB.Exec(`
		INSERT INTO user_assignments(user_id, entity_type, entity_id)
		VALUES ($1::uuid,'job',$2)
		ON CONFLICT (user_id, entity_type, entity_id) DO NOTHING
	`, access.UserID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign creator to job", "code": "JOB_ASSIGNMENT_FAILED"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *HiringHandler) ListJobs(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed", "code": "FORBIDDEN_SCOPE"})
		return
	}
	h.listJobsForAccess(c, access)
}

func (h *HiringHandler) ListJobsForIntake(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed", "code": "FORBIDDEN_SCOPE"})
		return
	}
	h.listJobsForAccess(c, access)
}

func (h *HiringHandler) listJobsForAccess(c *gin.Context, access hiringAccess) {

	query := `
		SELECT id::text, code, title, description, COALESCE(site_code,''), created_at
		FROM job_postings
	`
	args := []any{}
	if !access.Global {
		if access.AssignedOnly {
			query += ` WHERE id::text IN (SELECT entity_id FROM user_assignments WHERE user_id=$1::uuid AND entity_type='job') `
			args = append(args, access.UserID)
		} else {
			query += ` WHERE COALESCE(site_code,'')=$1 `
			args = append(args, access.SiteCode)
		}
	}
	query += `
		ORDER BY created_at DESC
		LIMIT 200
	`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load jobs", "code": "JOBS_QUERY_FAILED"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var id, code, title, desc, site string
		var createdAt time.Time
		if rows.Scan(&id, &code, &title, &desc, &site, &createdAt) == nil {
			out = append(out, gin.H{"id": id, "code": code, "title": title, "description": desc, "site_code": site, "created_at": createdAt})
		}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": out})
}

func (h *HiringHandler) ListApplications(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	query := `
		SELECT a.id::text, a.job_id::text, a.stage_code, a.source_type, a.created_at,
		       c.id::text, c.full_name, COALESCE(c.email,''), COALESCE(c.phone,''),
		       COALESCE(dh.risk_score,0), COALESCE(dh.triggers,'')
		FROM applications a
		JOIN job_postings j ON j.id=a.job_id
		JOIN candidates c ON c.id = a.candidate_id
		LEFT JOIN candidate_dedupe_hits dh ON dh.candidate_id=c.id
	`
	args := []any{}
	if !access.Global {
		if access.AssignedOnly {
			query += ` WHERE a.id::text IN (SELECT entity_id FROM user_assignments WHERE user_id=$1::uuid AND entity_type='application') `
			args = append(args, access.UserID)
		} else {
			query += ` WHERE COALESCE(j.site_code,'')=$1 `
			args = append(args, access.SiteCode)
		}
	}
	query += `
		ORDER BY a.created_at DESC
		LIMIT 300
	`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load applications"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var appID, jobID, stage, source, candidateID, fullName, email, phone, triggers string
		var createdAt time.Time
		var risk int
		if rows.Scan(&appID, &jobID, &stage, &source, &createdAt, &candidateID, &fullName, &email, &phone, &risk, &triggers) == nil {
			fullName = h.decryptCandidateValue(fullName)
			email = h.decryptCandidateValue(email)
			phone = h.decryptCandidateValue(phone)
			out = append(out, gin.H{
				"application_id": appID,
				"job_id":         jobID,
				"stage_code":     stage,
				"source_type":    source,
				"created_at":     createdAt,
				"candidate": gin.H{
					"id":        candidateID,
					"full_name": fullName,
					"email":     email,
					"phone":     phone,
				},
				"risk_score": risk,
				"triggers":   strings.Split(strings.TrimSpace(triggers), ","),
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"applications": out})
}

func (h *HiringHandler) ListPipelineTemplates(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT id::text, code, name, min_stages, max_stages, active, created_at
		FROM pipeline_templates
		ORDER BY created_at DESC
		LIMIT 100
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load pipeline templates"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var id, code, name string
		var minStages, maxStages int
		var active bool
		var createdAt time.Time
		if rows.Scan(&id, &code, &name, &minStages, &maxStages, &active, &createdAt) == nil {
			out = append(out, gin.H{"id": id, "code": code, "name": name, "min_stages": minStages, "max_stages": maxStages, "active": active, "created_at": createdAt})
		}
	}
	c.JSON(http.StatusOK, gin.H{"templates": out})
}

func (h *HiringHandler) GetPipelineTemplate(c *gin.Context) {
	id := c.Param("id")

	var tplID, code, name string
	var minStages, maxStages int
	var active bool
	var createdAt time.Time
	err := h.DB.QueryRow(`
		SELECT id::text, code, name, min_stages, max_stages, active, created_at
		FROM pipeline_templates WHERE id=$1::uuid
	`, id).Scan(&tplID, &code, &name, &minStages, &maxStages, &active, &createdAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pipeline template not found"})
		return
	}

	stageRows, err := h.DB.Query(`
		SELECT code, name, order_index, terminal, COALESCE(outcome,''), COALESCE(required_fields,'')
		FROM pipeline_stages
		WHERE template_id=$1::uuid
		ORDER BY order_index ASC
	`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load stages"})
		return
	}
	defer stageRows.Close()
	stages := []gin.H{}
	for stageRows.Next() {
		var sc, sn, outcome, reqFields string
		var orderIdx int
		var terminal bool
		if stageRows.Scan(&sc, &sn, &orderIdx, &terminal, &outcome, &reqFields) == nil {
			stages = append(stages, gin.H{"code": sc, "name": sn, "order_index": orderIdx, "terminal": terminal, "outcome": outcome, "required_fields": reqFields})
		}
	}

	transRows, err := h.DB.Query(`
		SELECT from_stage_code, to_stage_code, COALESCE(required_fields,''), COALESCE(screening_rule,'')
		FROM pipeline_transitions
		WHERE template_id=$1::uuid
		ORDER BY from_stage_code, to_stage_code
	`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load transitions"})
		return
	}
	defer transRows.Close()
	transitions := []gin.H{}
	for transRows.Next() {
		var from, to, req, rule string
		if transRows.Scan(&from, &to, &req, &rule) == nil {
			transitions = append(transitions, gin.H{"from_stage_code": from, "to_stage_code": to, "required_fields": req, "screening_rule": rule})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          tplID,
		"code":        code,
		"name":        name,
		"min_stages":  minStages,
		"max_stages":  maxStages,
		"active":      active,
		"created_at":  createdAt,
		"stages":      stages,
		"transitions": transitions,
	})
}

func (h *HiringHandler) UpdatePipelineTemplate(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Code        string           `json:"code"`
		Name        string           `json:"name"`
		Stages      []map[string]any `json:"stages"`
		Transitions []map[string]any `json:"transitions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	if err := h.Svc.ValidateDefinition(req.Stages, req.Transitions); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start update transaction"})
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE pipeline_templates SET code=$2, name=$3 WHERE id=$1::uuid`, id, req.Code, req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "template update failed"})
		return
	}
	_, _ = tx.Exec(`DELETE FROM pipeline_transitions WHERE template_id=$1::uuid`, id)
	_, _ = tx.Exec(`DELETE FROM pipeline_stages WHERE template_id=$1::uuid`, id)

	for _, st := range req.Stages {
		_, err = tx.Exec(`
			INSERT INTO pipeline_stages(id, template_id, code, name, order_index, terminal, outcome, required_fields)
			VALUES ($1,$2::uuid,$3,$4,$5,$6,$7,$8)
		`, uuid.NewString(), id,
			strings.ToUpper(strings.TrimSpace(toString(st["code"]))),
			toString(st["name"]),
			toIntVal(st["order_index"]),
			toBoolVal(st["terminal"]),
			strings.ToLower(strings.TrimSpace(toString(st["outcome"]))),
			toString(st["required_fields"]),
		)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "stage update failed"})
			return
		}
	}

	for _, tr := range req.Transitions {
		_, err = tx.Exec(`
			INSERT INTO pipeline_transitions(id, template_id, from_stage_code, to_stage_code, required_fields, screening_rule)
			VALUES ($1,$2::uuid,$3,$4,$5,$6)
		`, uuid.NewString(), id,
			strings.ToUpper(strings.TrimSpace(toString(tr["from_stage_code"]))),
			strings.ToUpper(strings.TrimSpace(toString(tr["to_stage_code"]))),
			toString(tr["required_fields"]),
			toString(tr["screening_rule"]),
		)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "transition update failed"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "template commit failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

func (h *HiringHandler) CreateManualApplication(c *gin.Context) {
	h.createApplication(c, "MANUAL")
}

func (h *HiringHandler) CreateKioskApplication(c *gin.Context) {
	h.createApplication(c, "KIOSK")
}

func (h *HiringHandler) createApplication(c *gin.Context, source string) {
	var req struct {
		JobID     string `json:"job_id"`
		FullName  string `json:"full_name"`
		Email     string `json:"email"`
		Phone     string `json:"phone"`
		SSN       string `json:"ssn"`
		StageCode string `json:"stage_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	rawIdentity := strings.ToLower(strings.TrimSpace(req.Email)) + "|" + normalizePhone(req.Phone)
	risk, dupTriggers, _ := h.Svc.ScoreDuplicate(rawIdentity, req.FullName)
	identityKey, err := h.Svc.IdentityToken(req.Email, req.Phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to tokenize candidate identity"})
		return
	}
	sev, blockTriggers, err := h.Svc.EvaluateBlocklist(req.Email, req.FullName, risk > 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "blocklist evaluation failed"})
		return
	}

	if sev == hiring.SeverityBlock {
		c.JSON(http.StatusForbidden, gin.H{
			"error":       "candidate blocked by policy",
			"severity":    string(sev),
			"block_rules": blockTriggers,
		})
		return
	}

	stage := strings.ToUpper(strings.TrimSpace(req.StageCode))
	if stage == "" {
		stage = "SCREENING"
	}
	storedSource := source
	if source == "KIOSK_PUBLIC" {
		storedSource = "KIOSK"
	}

	var actorID string
	if source != "KIOSK_PUBLIC" {
		actorID = c.GetString("userID")
	}
	var access hiringAccess
	if actorID != "" {
		access, err = h.loadAccess(c)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
			return
		}
		allowed, aerr := h.canAccessJob(access, req.JobID)
		if aerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job_id"})
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "job outside scope"})
			return
		}
	}

	var templateID sql.NullString
	_ = h.DB.QueryRow(`SELECT id::text FROM pipeline_templates WHERE active=true ORDER BY created_at DESC LIMIT 1`).Scan(&templateID)

	encSSN, err := h.PII.Encrypt(req.SSN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt sensitive data"})
		return
	}
	encFullName, err := h.encryptCandidateValue(req.FullName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt sensitive data"})
		return
	}
	encEmail, err := h.encryptCandidateValue(strings.ToLower(req.Email))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt sensitive data"})
		return
	}
	encPhone, err := h.encryptCandidateValue(normalizePhone(req.Phone))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt sensitive data"})
		return
	}
	nameToken, err := h.Svc.NameSearchToken(req.FullName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to tokenize candidate name"})
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction start failed"})
		return
	}
	defer tx.Rollback()

	candidateID := uuid.NewString()
	applicationID := uuid.NewString()
	_, err = tx.Exec(`
		INSERT INTO candidates(id, full_name, email, phone, ssn_raw, name_search_token, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
	`, candidateID, encFullName, encEmail, encPhone, encSSN, nameToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "candidate create failed"})
		return
	}

	_, err = tx.Exec(`INSERT INTO candidate_identities(id, candidate_id, identity_key, created_at) VALUES ($1,$2,$3,now())`, uuid.NewString(), candidateID, identityKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identity create failed"})
		return
	}

	_, err = tx.Exec(`
		INSERT INTO applications(id, candidate_id, job_id, source_type, stage_code, pipeline_template_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6::uuid,now())
	`, applicationID, candidateID, req.JobID, storedSource, stage, nullableTemplateID(templateID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "application create failed"})
		return
	}

	allTriggers := append(blockTriggers, dupTriggers...)
	_, _ = tx.Exec(`
		INSERT INTO candidate_dedupe_hits(id, candidate_id, risk_score, triggers, created_at)
		VALUES ($1,$2,$3,$4,now())
	`, uuid.NewString(), candidateID, risk, strings.Join(allTriggers, ","))

	_, _ = tx.Exec(`
		INSERT INTO application_pipeline_events(id, application_id, actor_id, to_stage_code, event_type, details)
		VALUES ($1,$2,$3,$4,'CREATE',$5::jsonb)
	`, uuid.NewString(), applicationID, nullableUUID(actorID), stage, `{"source":"`+source+`"}`)

	if actorID != "" {
		_, _ = tx.Exec(`
			INSERT INTO user_assignments(user_id, entity_type, entity_id)
			VALUES ($1::uuid,'application',$2), ($1::uuid,'candidate',$3), ($1::uuid,'job',$4)
			ON CONFLICT (user_id, entity_type, entity_id) DO NOTHING
		`, actorID, applicationID, candidateID, req.JobID)
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"application_id": applicationID,
		"candidate_id":   candidateID,
		"risk_score":     risk,
		"severity":       string(sev),
		"rule_triggers":  allTriggers,
	})
}

func (h *HiringHandler) ImportCSV(c *gin.Context) {
	var req struct {
		JobID string `json:"job_id"`
		CSV   string `json:"csv"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	count, err := h.Svc.ImportCSV(req.JobID, strings.NewReader(req.CSV))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"created": count})
}

func (h *HiringHandler) CreatePipelineTemplate(c *gin.Context) {
	var req struct {
		Code        string           `json:"code"`
		Name        string           `json:"name"`
		Stages      []map[string]any `json:"stages"`
		Transitions []map[string]any `json:"transitions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	id, err := h.Svc.ValidateAndSavePipeline(req.Code, req.Name, req.Stages, req.Transitions)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *HiringHandler) ValidatePipeline(c *gin.Context) {
	var req struct {
		Code        string           `json:"code"`
		Name        string           `json:"name"`
		Stages      []map[string]any `json:"stages"`
		Transitions []map[string]any `json:"transitions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	err := h.Svc.ValidateDefinition(req.Stages, req.Transitions)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true})
}

func (h *HiringHandler) TransitionApplication(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	if !h.applicationExists(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		return
	}
	allowed, err := h.canAccessApplication(access, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope resolution failed"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "application outside scope"})
		return
	}

	var req struct {
		FromStage string            `json:"from_stage"`
		ToStage   string            `json:"to_stage"`
		Fields    map[string]string `json:"fields"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	err = h.Svc.Transition(hiring.TransitionInput{
		ApplicationID: id,
		FromStage:     req.FromStage,
		ToStage:       req.ToStage,
		Provided:      req.Fields,
	}, c.GetString("userID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"transitioned": true})
}

func (h *HiringHandler) GetAllowedTransitions(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	if !h.applicationExists(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		return
	}
	allowed, err := h.canAccessApplication(access, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope resolution failed"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "application outside scope"})
		return
	}

	var currentStage string
	var templateID sql.NullString
	err = h.DB.QueryRow(`SELECT stage_code, COALESCE(pipeline_template_id::text,'') FROM applications WHERE id=$1::uuid`, id).Scan(&currentStage, &templateID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		return
	}

	fallback := map[string][]gin.H{
		"SCREENING":  {{"to_stage": "INVITATION", "required_fields": "notes"}},
		"INVITATION": {{"to_stage": "TEST", "required_fields": "invitation_date"}},
		"TEST":       {{"to_stage": "INTERVIEW", "required_fields": "test_score"}},
		"INTERVIEW":  {{"to_stage": "OFFER", "required_fields": "interview_result"}, {"to_stage": "REJECT", "required_fields": "reject_reason"}},
		"OFFER":      {{"to_stage": "HIRE", "required_fields": "offer_accept_date"}, {"to_stage": "REJECT", "required_fields": "reject_reason"}},
	}

	if !templateID.Valid || strings.TrimSpace(templateID.String) == "" {
		c.JSON(http.StatusOK, gin.H{"current_stage": currentStage, "allowed_transitions": fallback[strings.ToUpper(currentStage)]})
		return
	}

	rows, err := h.DB.Query(`
		SELECT to_stage_code, COALESCE(required_fields,''), COALESCE(screening_rule,'')
		FROM pipeline_transitions
		WHERE template_id=$1::uuid AND from_stage_code=$2
		ORDER BY to_stage_code
	`, templateID.String, strings.ToUpper(currentStage))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load allowed transitions"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var toStage, req, rule string
		if rows.Scan(&toStage, &req, &rule) == nil {
			out = append(out, gin.H{"to_stage": toStage, "required_fields": req, "screening_rule": rule})
		}
	}
	c.JSON(http.StatusOK, gin.H{"current_stage": currentStage, "allowed_transitions": out})
}

func (h *HiringHandler) GetPipelineEvents(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	if !h.applicationExists(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		return
	}
	allowed, err := h.canAccessApplication(access, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope resolution failed"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "application outside scope"})
		return
	}

	rows, err := h.DB.Query(`
		SELECT event_type, from_stage_code, to_stage_code, details::text, created_at
		FROM application_pipeline_events
		WHERE application_id=$1::uuid
		ORDER BY created_at ASC
	`, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to load events"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var eventType, from, to, details string
		var at time.Time
		if err := rows.Scan(&eventType, &from, &to, &details, &at); err == nil {
			out = append(out, gin.H{"event_type": eventType, "from": from, "to": to, "details": details, "at": at})
		}
	}
	c.JSON(http.StatusOK, gin.H{"events": out})
}

func (h *HiringHandler) GetCandidate(c *gin.Context) {
	access, err := h.loadAccess(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope resolution failed"})
		return
	}

	id := c.Param("id")
	if !h.candidateExists(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "candidate not found"})
		return
	}
	allowed, err := h.canAccessCandidate(access, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope resolution failed"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "candidate outside scope"})
		return
	}

	var fullName, email, phone, ssn string
	err = h.DB.QueryRow(`SELECT full_name, email, phone, COALESCE(ssn_raw,'') FROM candidates WHERE id=$1::uuid`, id).Scan(&fullName, &email, &phone, &ssn)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "candidate not found"})
		return
	}
	decSSN := ""
	if h.PII != nil {
		if plain, err := h.PII.Decrypt(ssn); err == nil {
			decSSN = plain
		}
	}
	fullName = h.decryptCandidateValue(fullName)
	email = h.decryptCandidateValue(email)
	phone = h.decryptCandidateValue(phone)
	c.JSON(http.StatusOK, gin.H{
		"id":        id,
		"full_name": fullName,
		"email":     email,
		"phone":     phone,
		"ssn":       masking.MaskSSN(decSSN),
	})
}

func (h *HiringHandler) CreatePublicKioskApplication(c *gin.Context) {
	h.createApplication(c, "KIOSK_PUBLIC")
}

func (h *HiringHandler) ListPublicKioskJobs(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT id::text, code, title
		FROM job_postings
		ORDER BY created_at DESC
		LIMIT 200
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load jobs"})
		return
	}
	defer rows.Close()

	jobs := []gin.H{}
	for rows.Next() {
		var id, code, title string
		if rows.Scan(&id, &code, &title) == nil {
			jobs = append(jobs, gin.H{"id": id, "code": code, "title": title})
		}
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

func (h *HiringHandler) CreateBlocklistRule(c *gin.Context) {
	var req struct {
		RuleType string `json:"rule_type"`
		Pattern  string `json:"pattern"`
		Severity string `json:"severity"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	sev := strings.ToLower(strings.TrimSpace(req.Severity))
	if sev != "info" && sev != "warn" && sev != "block" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid severity"})
		return
	}
	_, err := h.DB.Exec(`
		INSERT INTO blocklist_rules(id, rule_type, pattern, severity, active, created_at)
		VALUES ($1,$2,$3,$4,true,now())
	`, uuid.NewString(), strings.ToLower(req.RuleType), strings.ToLower(req.Pattern), sev)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to create blocklist rule"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"created": true})
}

func normalizePhone(in string) string {
	b := strings.Builder{}
	for _, r := range in {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func nullableUUID(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableTemplateID(v sql.NullString) any {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil
	}
	return v.String
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func toIntVal(v any) int {
	var x int
	_, _ = fmt.Sscan(fmt.Sprint(v), &x)
	return x
}

func toBoolVal(v any) bool {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
	return s == "true" || s == "1" || s == "yes"
}

func (h *HiringHandler) encryptCandidateValue(value string) (string, error) {
	if h.PII == nil {
		return value, nil
	}
	return h.PII.Encrypt(value)
}

func (h *HiringHandler) decryptCandidateValue(value string) string {
	if strings.TrimSpace(value) == "" || h.PII == nil {
		return value
	}
	plain, err := h.PII.Decrypt(value)
	if err != nil {
		return value
	}
	return plain
}
