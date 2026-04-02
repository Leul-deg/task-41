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
)

func TestGetTicket_DeniesOutsideSiteScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery(`SELECT COALESCE\(calendar_site_code`).WithArgs("ticket-1").WillReturnRows(
		sqlmock.NewRows([]string{"calendar_site_code", "assignee_id"}).AddRow("SITE-B", sql.NullString{}),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Next()
	})
	r.GET("/tickets/:id", h.GetTicket)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tickets/ticket-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d", w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListOrders_FiltersToSiteForNonGlobalScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery("FROM orders").WithArgs("SITE-A").WillReturnRows(
		sqlmock.NewRows([]string{"id", "customer_ref", "site_code", "created_at"}).AddRow("ORD-1001", "CUST-1", "SITE-A", time.Now()),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Next()
	})
	r.GET("/orders", h.ListOrders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ORD-1001") {
		t.Fatalf("expected ORD-1001 in response, got: %s", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListOrders_GlobalScopeReturnsOrders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)
	mock.ExpectQuery("FROM orders").WillReturnRows(
		sqlmock.NewRows([]string{"id", "customer_ref", "site_code", "created_at"}).AddRow("ORD-2002", "CUST-2", "SITE-B", time.Now()),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Next()
	})
	r.GET("/orders", h.ListOrders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ORD-2002") {
		t.Fatalf("expected ORD-2002 in response, got: %s", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
