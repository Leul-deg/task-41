package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"meridian/backend/internal/http/middleware"
	"meridian/backend/internal/platform/security"
)

func TestExportAuditLogs_MasksSensitivePatterns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewComplianceHandler(db)

	rows := sqlmock.NewRows([]string{"actor_id", "action_class", "entity_type", "entity_id", "event_data", "created_at"}).
		AddRow("u1", "EXPORT", "candidate", "c1", `{"ssn":"123-45-6789"}`, time.Now())
	mock.ExpectQuery("SELECT actor_id, action_class, entity_type, entity_id, event_data::text, created_at").
		WithArgs(500).
		WillReturnRows(rows)

	r := gin.New()
	r.GET("/x", h.ExportAuditLogs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x?format=csv", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "***-**-6789") {
		t.Fatalf("expected masked ssn in export, got %s", body)
	}
}

func TestProcessDeletionRequest_RequiresStepUpToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewComplianceHandler(db)
	tokens := security.NewTokenManager("test-secret")

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxUserID, "11111111-1111-1111-1111-111111111111")
		c.Next()
	})
	r.POST("/deletion/:id/process", middleware.RequireStepUp(tokens, "delete_or_reversal"), h.ProcessDeletionRequest)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deletion/11111111-1111-1111-1111-111111111111/process", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without step-up token, got %d", w.Code)
	}

	stepUp, err := tokens.CreateStepUpToken("11111111-1111-1111-1111-111111111111", "delete_or_reversal", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT COALESCE\(policy_result,''\), subject_ref, status`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"policy", "subject_ref", "status"}).AddRow("anonymize", "11111111-1111-1111-1111-111111111111", "PENDING"),
	)
	mock.ExpectExec("UPDATE candidates SET full_name='ANONYMIZED'").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE deletion_requests SET status='COMPLETED'").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/deletion/11111111-1111-1111-1111-111111111111/process", nil)
	req2.Header.Set("X-Step-Up-Token", stepUp)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid step-up token, got %d", w2.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
