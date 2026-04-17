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

	h := NewAdminHandler(db, nil)
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

	h := NewAdminHandler(db, nil)
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

func TestListRoles_ReturnsSortedList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewAdminHandler(db, nil)
	mock.ExpectQuery("FROM roles ORDER BY code").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name"}).
			AddRow("role-1", "ADMIN", "Administrator").
			AddRow("role-2", "SUPPORT", "Support Agent"))

	r := gin.New()
	r.GET("/roles", h.ListRoles)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/roles", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "ADMIN") || !strings.Contains(body, "SUPPORT") {
		t.Fatalf("expected roles in response, got %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRolePermissions_SuccessPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewAdminHandler(db, nil)
	roleID := "11111111-1111-1111-1111-111111111111"

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM role_permissions`).WithArgs(roleID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id::text FROM permissions`).
		WithArgs("hiring", "view").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("perm-1"))
	mock.ExpectExec(`INSERT INTO role_permissions`).
		WithArgs(sqlmock.AnyArg(), roleID, "perm-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	r := gin.New()
	r.PUT("/roles/:id/permissions", h.UpdateRolePermissions)

	body := `{"permissions":[{"module":"hiring","action":"view"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/roles/"+roleID+"/permissions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"updated":true`) {
		t.Fatalf("expected updated:true in response, got %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRoleScopes_SuccessPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewAdminHandler(db, nil)
	roleID := "11111111-1111-1111-1111-111111111111"

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM scope_rules`).WithArgs(roleID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO scope_rules`).
		WithArgs(sqlmock.AnyArg(), roleID, "hiring", "site", "SITE-A").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	r := gin.New()
	r.PUT("/roles/:id/scopes", h.UpdateRoleScopes)

	body := `{"scopes":[{"module":"hiring","scope":"site","value":"SITE-A"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/roles/"+roleID+"/scopes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"updated":true`) {
		t.Fatalf("expected updated:true in response, got %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRotateClientKey_Success_Returns201(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewAdminHandler(db, nil)

	mock.ExpectExec("INSERT INTO client_keys").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "test-secret").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO key_rotation_events").
		WithArgs("test-key", "user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.POST("/client-keys/rotate", h.RotateClientKey)

	body := `{"key_name":"test-key","secret":"test-secret"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/client-keys/rotate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"key_id"`) {
		t.Fatalf("expected key_id in response, got: %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRevokeClientKey_Success_Returns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewAdminHandler(db, nil)

	mock.ExpectExec("UPDATE client_keys").WithArgs("test-key-v123").WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.POST("/client-keys/:id/revoke", h.RevokeClientKey)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/client-keys/test-key-v123/revoke", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"revoked":true`) {
		t.Fatalf("expected revoked:true in response, got: %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
