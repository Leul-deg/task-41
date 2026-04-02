package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"meridian/backend/internal/platform/security"
)

func TestRequireSignedRequests_RejectsMissingHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, _ := sqlmock.New()
	defer db.Close()

	r := gin.New()
	r.Use(RequireSignedRequests(db))
	r.GET("/api/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestRequireSignedRequests_AllowsValidAndRejectsReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	secret := "abc123"
	mock.ExpectQuery("SELECT secret FROM client_keys").WithArgs("k1").WillReturnRows(sqlmock.NewRows([]string{"secret"}).AddRow(secret))
	mock.ExpectExec("INSERT INTO request_nonces").WithArgs("k1", "n-1").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT secret FROM client_keys").WithArgs("k1").WillReturnRows(sqlmock.NewRows([]string{"secret"}).AddRow(secret))
	mock.ExpectExec("INSERT INTO request_nonces").WithArgs("k1", "n-1").WillReturnError(io.EOF)

	r := gin.New()
	r.Use(RequireSignedRequests(db))
	r.GET("/api/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	ts := time.Now().UTC().Format(time.RFC3339)
	sig := security.ComputeSignature(secret, http.MethodGet, "/api/ping", ts, "n-1", security.PayloadHash([]byte{}))

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req1.Header.Set("X-Client-Key", "k1")
	req1.Header.Set("X-Timestamp", ts)
	req1.Header.Set("X-Nonce", "n-1")
	req1.Header.Set("X-Signature", sig)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w1.Code)
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req2.Header.Set("X-Client-Key", "k1")
	req2.Header.Set("X-Timestamp", ts)
	req2.Header.Set("X-Nonce", "n-1")
	req2.Header.Set("X-Signature", sig)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 got %d", w2.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
