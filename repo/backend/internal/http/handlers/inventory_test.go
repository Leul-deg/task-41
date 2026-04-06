package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestGetBalances_UsesRoleAwareThresholdAndAvailableStock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewInventoryHandler(db)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code", "warehouse_code"}).AddRow("SITE-A", "WH-1"),
	)
	mock.ExpectQuery(`SELECT scope, COALESCE\(scope_value,''\)`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope", "scope_value"}).AddRow("warehouse", "WH-1"),
	)
	mock.ExpectQuery("SELECT ib.warehouse_code").WithArgs("SITE-A", "WH-1").WillReturnRows(
		sqlmock.NewRows([]string{"warehouse_code", "sub_warehouse_code", "sku", "on_hand", "reserved"}).
			AddRow("WH-1", "A2", "SKU-100", 25, 12),
	)
	mock.ExpectQuery("SELECT COALESCE").WithArgs("user-1", "SITE-A", "SKU-100").WillReturnRows(
		sqlmock.NewRows([]string{"coalesce"}).AddRow(20),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Next()
	})
	r.GET("/balances", h.GetBalances)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/balances?site=SITE-A", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !(strings.Contains(body, `"available":13`) && strings.Contains(body, `"safety_stock":20`) && strings.Contains(body, `"low_stock_alert":true`)) {
		t.Fatalf("unexpected response body: %s", body)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMoveOutbound_DeniesCrossSiteWarehouse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewInventoryHandler(db)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code", "warehouse_code"}).AddRow("SITE-A", ""),
	)
	mock.ExpectQuery(`SELECT scope, COALESCE\(scope_value,''\)`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope", "scope_value"}).AddRow("site", "SITE-A"),
	)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM warehouses WHERE code=\$1 AND site_code=\$2`).WithArgs("WH-Z", "SITE-A").WillReturnRows(
		sqlmock.NewRows([]string{"count"}).AddRow(0),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Next()
	})
	r.POST("/outbound", h.MoveOutbound)

	payload := `{"sku":"SKU-100","quantity":1,"from_warehouse":"WH-Z","reason_code":"ORDER"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/outbound", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListOrders_FiltersToSiteForNonGlobalScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewInventoryHandler(db)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code", "warehouse_code"}).AddRow("SITE-A", ""),
	)
	mock.ExpectQuery(`SELECT scope, COALESCE\(scope_value,''\)`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope", "scope_value"}).AddRow("site", "SITE-A"),
	)
	mock.ExpectQuery("FROM orders").WithArgs("SITE-A").WillReturnRows(
		sqlmock.NewRows([]string{"id", "customer_ref", "site_code", "created_at"}).AddRow("ORD-3001", "CUST-1", "SITE-A", time.Now()),
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
	if !strings.Contains(w.Body.String(), "ORD-3001") {
		t.Fatalf("expected ORD-3001 in response, got: %s", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListOrders_GlobalScopeReturnsOrders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewInventoryHandler(db)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code", "warehouse_code"}).AddRow("SITE-A", "WH-1"),
	)
	mock.ExpectQuery(`SELECT scope, COALESCE\(scope_value,''\)`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope", "scope_value"}).AddRow("global", ""),
	)
	mock.ExpectQuery("FROM orders").WillReturnRows(
		sqlmock.NewRows([]string{"id", "customer_ref", "site_code", "created_at"}).AddRow("ORD-3002", "CUST-2", "SITE-B", time.Now()),
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
	if !strings.Contains(w.Body.String(), "ORD-3002") {
		t.Fatalf("expected ORD-3002 in response, got: %s", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReverseLedger_CreatesCompensatingEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewInventoryHandler(db)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code", "warehouse_code"}).AddRow("SITE-A", "WH-1"),
	)
	mock.ExpectQuery(`SELECT scope, COALESCE\(scope_value,''\)`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope", "scope_value"}).AddRow("global", ""),
	)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT movement_type, sku, quantity, warehouse_code FROM inventory_ledger`).WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"movement_type", "sku", "quantity", "warehouse_code"}).AddRow("OUTBOUND", "SKU-100", -2, "WH-1"),
	)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM ledger_reversals`).WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"exists"}).AddRow(false),
	)
	mock.ExpectExec(`UPDATE inventory_balances`).WithArgs("WH-1", "SKU-100", 2).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO inventory_ledger`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO ledger_reversals`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "user-1")
		c.Next()
	})
	r.POST("/ledger/:id/reverse", h.ReverseLedger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ledger/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/reverse", bytes.NewBufferString(`{"reason_code":"CORRECTION"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"reversed":true`) {
		t.Fatalf("expected reversed response, got: %s", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
