package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"meridian/backend/internal/platform/security"
)

func TestValidatePipeline_IsSideEffectFree(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	r := gin.New()
	r.POST("/validate", h.ValidatePipeline)

	payload := `{
		"name":"QA Pipeline",
		"stages":[
			{"code":"SCREENING","name":"Screening","order_index":1,"terminal":false,"outcome":""},
			{"code":"HIRE","name":"Hire","order_index":2,"terminal":true,"outcome":"success"},
			{"code":"REJECT","name":"Reject","order_index":3,"terminal":true,"outcome":"failure"}
		],
		"transitions":[
			{"from_stage_code":"SCREENING","to_stage_code":"HIRE"},
			{"from_stage_code":"SCREENING","to_stage_code":"REJECT"}
		]
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/validate", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expected no DB mutation/query, got %v", err)
	}
}

func TestGetAllowedTransitions_ScopeIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "11111111-1111-1111-1111-111111111111")
		c.Next()
	})
	r.GET("/applications/:id/allowed", h.GetAllowedTransitions)

	appID := "22222222-2222-2222-2222-222222222222"

	mock.ExpectQuery(`SELECT COALESCE\(site_code,'SITE-A'\) FROM users`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM applications`).WithArgs(appID).WillReturnRows(
		sqlmock.NewRows([]string{"exists"}).AddRow(true),
	)
	mock.ExpectQuery(`SELECT COALESCE\(j.site_code,''\)`).WithArgs(appID).WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-B"),
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/applications/"+appID+"/allowed", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for out-of-scope application, got %d body=%s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetAllowedTransitions_NotFoundWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "11111111-1111-1111-1111-111111111111")
		c.Next()
	})
	r.GET("/applications/:id/allowed", h.GetAllowedTransitions)

	appID := "33333333-3333-3333-3333-333333333333"
	mock.ExpectQuery(`SELECT COALESCE\(site_code,'SITE-A'\) FROM users`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM applications`).WithArgs(appID).WillReturnRows(
		sqlmock.NewRows([]string{"exists"}).AddRow(false),
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/applications/"+appID+"/allowed", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing application, got %d body=%s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCanAccessApplication_AssignedAndGlobal(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)

	allowed, err := h.canAccessApplication(hiringAccess{Global: true}, "any")
	if err != nil || !allowed {
		t.Fatalf("expected global access true, got allowed=%v err=%v", allowed, err)
	}

	mock.ExpectQuery(`SELECT EXISTS\(`).WithArgs("11111111-1111-1111-1111-111111111111", "application", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"exists"}).AddRow(true),
	)
	allowed, err = h.canAccessApplication(hiringAccess{UserID: "11111111-1111-1111-1111-111111111111", AssignedOnly: true}, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	if err != nil || !allowed {
		t.Fatalf("expected assigned access true, got allowed=%v err=%v", allowed, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListJobs_ScopeFailureReturnsCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	r := gin.New()
	r.GET("/jobs", h.ListJobs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":"FORBIDDEN_SCOPE"`) {
		t.Fatalf("expected FORBIDDEN_SCOPE code, got body=%s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListJobs_QueryFailureReturnsCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "11111111-1111-1111-1111-111111111111")
		c.Next()
	})
	r.GET("/jobs", h.ListJobs)

	mock.ExpectQuery(`SELECT COALESCE\(site_code,'SITE-A'\) FROM users`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery("SELECT id::text, code, title").WithArgs("SITE-A").WillReturnError(errors.New("db down"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":"JOBS_QUERY_FAILED"`) {
		t.Fatalf("expected JOBS_QUERY_FAILED code, got body=%s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHiringListJobs_GlobalScopeReturnsAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, security.NewPIIProtector(nil, "PII_TEST", "test-seed"))
	mock.ExpectQuery(`SELECT COALESCE\(site_code,'SITE-A'\) FROM users`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)
	mock.ExpectQuery("FROM job_postings").WillReturnRows(
		sqlmock.NewRows([]string{"id", "code", "title", "description", "site_code", "created_at"}).
			AddRow("job-1", "JOB-001", "Test Job", "desc", "SITE-A", time.Now()),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.GET("/jobs", h.ListJobs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "JOB-001") {
		t.Fatalf("expected JOB-001 in response, got: %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHiringListJobs_SiteScopeFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, security.NewPIIProtector(nil, "PII_TEST", "test-seed"))
	mock.ExpectQuery(`SELECT COALESCE\(site_code,'SITE-A'\) FROM users`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery("FROM job_postings").WithArgs("SITE-A").WillReturnRows(
		sqlmock.NewRows([]string{"id", "code", "title", "description", "site_code", "created_at"}).
			AddRow("job-2", "JOB-002", "Site Job", "site-desc", "SITE-A", time.Now()),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.GET("/jobs", h.ListJobs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "JOB-002") {
		t.Fatalf("expected JOB-002 in response, got: %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHiringListPipelineTemplates_ReturnsList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	mock.ExpectQuery("FROM pipeline_templates").WillReturnRows(
		sqlmock.NewRows([]string{"id", "code", "name", "min_stages", "max_stages", "active", "created_at"}).
			AddRow("tpl-1", "STANDARD", "Standard Pipeline", 3, 8, true, time.Now()),
	)

	r := gin.New()
	r.GET("/pipelines/templates", h.ListPipelineTemplates)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pipelines/templates", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "STANDARD") {
		t.Fatalf("expected STANDARD in response, got: %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHiringCreateBlocklistRule_InvalidSeverity_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)

	r := gin.New()
	r.POST("/blocklist/rules", h.CreateBlocklistRule)

	body := `{"rule_type":"email","pattern":"spam@example.com","severity":"invalid"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/blocklist/rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHiringCreateBlocklistRule_ValidRule_Returns201(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	mock.ExpectExec("INSERT INTO blocklist_rules").
		WithArgs(sqlmock.AnyArg(), "email", "spam@test.com", "block").
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.POST("/blocklist/rules", h.CreateBlocklistRule)

	body := `{"rule_type":"email","pattern":"spam@test.com","severity":"block"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/blocklist/rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHiringGetCandidate_NotExists_Returns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, security.NewPIIProtector(nil, "PII_TEST", "test-seed"))
	mock.ExpectQuery(`SELECT COALESCE\(site_code,'SITE-A'\) FROM users`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM candidates`).WithArgs("cand-99").WillReturnRows(
		sqlmock.NewRows([]string{"exists"}).AddRow(false),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.GET("/candidates/:id", h.GetCandidate)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/candidates/cand-99", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHiringTransitionApplication_NotFound_Returns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	mock.ExpectQuery(`SELECT COALESCE\(site_code,'SITE-A'\) FROM users`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM applications`).WithArgs("app-99").WillReturnRows(
		sqlmock.NewRows([]string{"exists"}).AddRow(false),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.POST("/applications/:id/transition", h.TransitionApplication)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/applications/app-99/transition", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestImportCSV_DeniesOutOfScopeJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewHiringHandler(db, false, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "11111111-1111-1111-1111-111111111111")
		c.Next()
	})
	r.POST("/applications/import-csv", h.ImportCSV)

	mock.ExpectQuery(`SELECT COALESCE\(site_code,'SITE-A'\) FROM users`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery(`SELECT COALESCE\(site_code,''\) FROM job_postings WHERE id=\$1::uuid`).WithArgs("22222222-2222-2222-2222-222222222222").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-B"),
	)

	body := `{"job_id":"22222222-2222-2222-2222-222222222222","csv":"full_name,email,phone\nTest User,test@example.com,5551234567\n"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/applications/import-csv", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
