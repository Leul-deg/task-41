package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestUpdateRolePermissions_FailsWhenResetFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewAdminHandler(db)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM role_permissions`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()

	r := gin.New()
	r.PUT("/roles/:id/permissions", h.UpdateRolePermissions)

	body := `{"permissions":[{"module":"hiring","action":"view"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/roles/11111111-1111-1111-1111-111111111111/permissions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRoleScopes_FailsWhenInsertFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewAdminHandler(db)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM scope_rules`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scope_rules`).WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()

	r := gin.New()
	r.PUT("/roles/:id/scopes", h.UpdateRoleScopes)

	body := `{"scopes":[{"module":"hiring","scope":"site","value":"SITE-A"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/roles/11111111-1111-1111-1111-111111111111/scopes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
