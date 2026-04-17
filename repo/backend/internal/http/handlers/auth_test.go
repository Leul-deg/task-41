package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"meridian/backend/internal/platform/security"
)

func newTestAuthHandler(db *sql.DB) *AuthHandler {
	return NewAuthHandler(
		db,
		security.NewTokenManager("test-secret"),
		security.NewPIIProtector(nil, "PII_TEST", "test-seed"),
		time.Hour,
		24*time.Hour,
		time.Minute,
	)
}

func TestLogin_ShortPasswordReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestAuthHandler(nil)

	r := gin.New()
	r.POST("/login", h.Login)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"username":"alice","password":"short"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLogin_UserNotFoundReturns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	mock.ExpectQuery("SELECT id, username, password_hash, failed_attempts, locked_until").
		WithArgs("alice").
		WillReturnError(sql.ErrNoRows)

	r := gin.New()
	r.POST("/login", h.Login)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"username":"alice","password":"validPassword123"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLogin_AccountLockedReturns423(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	mock.ExpectQuery("SELECT id, username, password_hash, failed_attempts, locked_until").
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "failed_attempts", "locked_until"}).
			AddRow("user-1", "alice", "irrelevant", 5, time.Now().Add(10*time.Minute)))

	r := gin.New()
	r.POST("/login", h.Login)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"username":"alice","password":"validPassword123"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusLocked {
		t.Fatalf("expected 423 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLogin_WrongPasswordIncrementsCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	hash, err := security.HashPassword("correctPassword123!")
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("SELECT id, username, password_hash, failed_attempts, locked_until").
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "failed_attempts", "locked_until"}).
			AddRow("user-1", "alice", hash, 0, nil))
	mock.ExpectExec("UPDATE users SET failed_attempts=").
		WithArgs(sqlmock.AnyArg(), "user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.POST("/login", h.Login)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"username":"alice","password":"wrongPassword123!"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLogin_FifthFailLocksAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	hash, err := security.HashPassword("correctPassword123!")
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("SELECT id, username, password_hash, failed_attempts, locked_until").
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "failed_attempts", "locked_until"}).
			AddRow("user-1", "alice", hash, 4, nil))
	mock.ExpectExec("UPDATE users SET failed_attempts=0, locked_until=now").
		WithArgs("user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.POST("/login", h.Login)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"username":"alice","password":"wrongPassword123!"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLogin_SuccessReturns200WithTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	hash, err := security.HashPassword("correctPassword123!")
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("SELECT id, username, password_hash, failed_attempts, locked_until").
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "failed_attempts", "locked_until"}).
			AddRow("user-1", "alice", hash, 0, nil))
	mock.ExpectExec("UPDATE users SET failed_attempts=0, locked_until=NULL").
		WithArgs("user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO refresh_tokens").
		WithArgs(sqlmock.AnyArg(), "user-1", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.POST("/login", h.Login)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"username":"alice","password":"correctPassword123!"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "access_token") || !strings.Contains(body, "refresh_token") {
		t.Fatalf("expected tokens in response, got %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRefresh_InvalidTokenReturns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	mock.ExpectQuery("SELECT u.id, u.username").
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)

	r := gin.New()
	r.POST("/refresh", h.Refresh)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/refresh",
		strings.NewReader(`{"refresh_token":"bogus-token-value"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRefresh_ValidTokenReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	mock.ExpectQuery("SELECT u.id, u.username").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username"}).AddRow("user-1", "alice"))

	r := gin.New()
	r.POST("/refresh", h.Refresh)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/refresh",
		strings.NewReader(`{"refresh_token":"any-refresh-token"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "access_token") {
		t.Fatalf("expected access_token in response, got %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStepUp_MissingActorReturns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestAuthHandler(nil)

	r := gin.New()
	r.POST("/step-up", h.StepUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/step-up",
		strings.NewReader(`{"password":"somePassword123","action_class":"delete_or_reversal"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStepUp_WrongPasswordReturns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	hash, err := security.HashPassword("correctPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT password_hash FROM users WHERE id=").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(hash))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.POST("/step-up", h.StepUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/step-up",
		strings.NewReader(`{"password":"wrongPassword123!","action_class":"delete_or_reversal"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStepUp_SuccessReturns200WithToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	hash, err := security.HashPassword("correctPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT password_hash FROM users WHERE id=").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(hash))
	mock.ExpectExec("INSERT INTO step_up_tokens").
		WithArgs(sqlmock.AnyArg(), "user-1", "delete_or_reversal", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.POST("/step-up", h.StepUp)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/step-up",
		strings.NewReader(`{"password":"correctPassword123!","action_class":"delete_or_reversal"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "step_up_token") {
		t.Fatalf("expected step_up_token in response, got %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMe_MissingActorReturns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestAuthHandler(nil)

	r := gin.New()
	r.GET("/me", h.Me)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestMe_UnknownUserReturns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	mock.ExpectQuery("SELECT username FROM users WHERE id=").
		WithArgs("user-unknown").
		WillReturnError(sql.ErrNoRows)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-unknown"); c.Next() })
	r.GET("/me", h.Me)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMe_ReturnsUserProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := newTestAuthHandler(db)
	mock.ExpectQuery("SELECT username FROM users WHERE id=").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"username"}).AddRow("alice"))
	mock.ExpectQuery("JOIN roles r").WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("ADMIN"))
	mock.ExpectQuery("JOIN role_permissions rp").WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"module", "action"}).AddRow("hiring", "view"))
	mock.ExpectQuery("JOIN scope_rules sr").WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"module", "scope", "scope_value"}).AddRow("support", "global", ""))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.GET("/me", h.Me)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "alice") || !strings.Contains(body, "ADMIN") {
		t.Fatalf("expected profile data in response, got %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
