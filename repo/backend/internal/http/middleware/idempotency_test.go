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
