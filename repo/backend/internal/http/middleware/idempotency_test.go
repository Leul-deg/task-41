package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestRequireIdempotency_ReplaysStoredResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery("SELECT status_code, response_body").
		WithArgs("/api/inventory/reservations/order-create", "u1", "h1", "idem-1").
		WillReturnRows(sqlmock.NewRows([]string{"status_code", "response_body"}).AddRow(201, `{"id":"r-1"}`))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxUserID, "u1")
		c.Set("payloadHash", "h1")
		c.Next()
	})
	r.Use(RequireIdempotency(db, time.Hour))
	r.POST("/api/inventory/reservations/order-create", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"id": "should-not-run"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inventory/reservations/order-create", nil)
	req.Header.Set("Idempotency-Key", "idem-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d", w.Code)
	}
	if got := w.Body.String(); got != `{"id":"r-1"}` {
		t.Fatalf("unexpected body: %s", got)
	}
}

func TestRequireIdempotency_MissingKey_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, _ := sqlmock.New()
	defer db.Close()

	r := gin.New()
	r.Use(RequireIdempotency(db, time.Hour))
	r.POST("/api/inventory/reservations/order-create", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/inventory/reservations/order-create", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRequireIdempotency_CacheMiss_CallsHandlerAndStoresResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery("SELECT status_code, response_body").
		WithArgs("/api/support/tickets/refund-approve", "u2", "h2", "idem-new").
		WillReturnRows(sqlmock.NewRows([]string{"status_code", "response_body"}))
	mock.ExpectExec("INSERT INTO idempotency_records").
		WithArgs(
			"/api/support/tickets/refund-approve",
			"u2",
			"h2",
			"idem-new",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxUserID, "u2")
		c.Set("payloadHash", "h2")
		c.Next()
	})
	r.Use(RequireIdempotency(db, time.Hour))
	r.POST("/api/support/tickets/refund-approve", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/support/tickets/refund-approve", nil)
	req.Header.Set("Idempotency-Key", "idem-new")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
