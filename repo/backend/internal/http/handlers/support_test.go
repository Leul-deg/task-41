package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"meridian/backend/internal/platform/security"
)

func TestGetTicket_DeniesOutsideSiteScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery(`SELECT COALESCE\(calendar_site_code`).WithArgs("ticket-1").WillReturnRows(
		sqlmock.NewRows([]string{"calendar_site_code", "assignee_id", "order_id"}).AddRow("SITE-B", sql.NullString{}, "ORD-1001"),
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

func TestSupportListOrders_FiltersToSiteForNonGlobalScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

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

func TestSupportListOrders_GlobalScopeReturnsOrders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

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

func TestSupportListOrders_AssignedScopeUsesExplicitAssignments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("assigned"),
	)
	mock.ExpectQuery("FROM orders").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"id", "customer_ref", "site_code", "created_at"}).AddRow("ORD-ASSIGNED", "CUST-9", "SITE-A", time.Now()),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "11111111-1111-1111-1111-111111111111")
		c.Next()
	})
	r.GET("/orders", h.ListOrders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ORD-ASSIGNED") {
		t.Fatalf("expected assigned order in response, got: %s", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSupportListOrdersForIntake_AssignedScopeUsesSiteOrders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("assigned"),
	)
	mock.ExpectQuery("FROM orders").WithArgs("SITE-A").WillReturnRows(
		sqlmock.NewRows([]string{"id", "customer_ref", "site_code", "created_at"}).AddRow("ORD-SITE", "CUST-10", "SITE-A", time.Now()),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "11111111-1111-1111-1111-111111111111")
		c.Next()
	})
	r.GET("/orders/for-intake", h.ListOrders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orders/for-intake", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ORD-SITE") {
		t.Fatalf("expected site order in response, got: %s", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTicket_InvalidType_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.POST("/tickets", h.CreateTicket)

	body := `{"order_id":"ord-1","ticket_type":"invalid","priority":"HIGH","description":"test"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateTicket_VersionConflict_Returns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)
	mock.ExpectQuery("SELECT record_version FROM support_tickets").WithArgs("ticket-99").WillReturnRows(
		sqlmock.NewRows([]string{"record_version"}).AddRow(5),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.PATCH("/tickets/:id", h.UpdateTicket)

	body := `{"description":"updated","record_version":3}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/tickets/ticket-99", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateTicket_Success_Returns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)
	mock.ExpectQuery("SELECT record_version FROM support_tickets").WithArgs("ticket-99").WillReturnRows(
		sqlmock.NewRows([]string{"record_version"}).AddRow(5),
	)
	mock.ExpectExec("UPDATE support_tickets").
		WithArgs("ticket-99", "new description").
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.PATCH("/tickets/:id", h.UpdateTicket)

	body := `{"description":"new description","record_version":5}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/tickets/ticket-99", strings.NewReader(body))
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

func TestSupportListTickets_GlobalScopeReturnsAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)
	mock.ExpectQuery("FROM support_tickets").WillReturnRows(
		sqlmock.NewRows([]string{"id", "order_id", "ticket_type", "priority", "status", "record_version", "sla_due_at", "escalated", "created_at"}).
			AddRow("t-1", "ORD-1001", "return_and_refund", "HIGH", "OPEN", 1, time.Now(), false, time.Now()),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.GET("/tickets", h.ListTickets)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ORD-1001") {
		t.Fatalf("expected ORD-1001 in response, got: %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApproveRefund_MissingTicket_Returns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("site"),
	)
	mock.ExpectQuery(`SELECT COALESCE\(calendar_site_code`).WithArgs("ticket-missing").WillReturnRows(
		sqlmock.NewRows([]string{"calendar_site_code", "assignee_id", "order_id"}),
	)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.POST("/tickets/refund-approve", h.ApproveRefund)

	body := `{"ticket_id":"ticket-missing","note":"test"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tickets/refund-approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApproveRefund_GlobalScope_Success_Returns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("user-1").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("global"),
	)
	mock.ExpectExec("UPDATE support_tickets").WithArgs("ticket-1").WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", "user-1"); c.Next() })
	r.POST("/tickets/refund-approve", h.ApproveRefund)

	body := `{"ticket_id":"ticket-1","note":"approved"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tickets/refund-approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"approved":true`) {
		t.Fatalf("expected approved:true in response, got: %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistSupportAttachment_EncryptsBytesAtRest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ATTACHMENT_STORAGE_DIR", tmp)

	h := NewSupportHandler(nil, security.NewPIIProtector(nil, "PII_TEST", "test-seed"))
	path, err := h.persistSupportAttachment("ticket-1", "attachment-1", "proof.pdf", "application/pdf", []byte("plain-proof-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".pdf" {
		t.Fatalf("expected .pdf extension, got %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "plain-proof-bytes") {
		t.Fatalf("expected stored payload to be encrypted at rest")
	}
}

func TestCreateTicket_AssignedScopeAllowsSiteOrderAndAutoAssigns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	h := NewSupportHandler(db, nil)

	mock.ExpectQuery(`SELECT COALESCE\(site_code`).WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT scope").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"scope"}).AddRow("assigned"),
	)
	mock.ExpectQuery(`SELECT site_code FROM orders WHERE id=\$1`).WithArgs("ORD-1001").WillReturnRows(
		sqlmock.NewRows([]string{"site_code"}).AddRow("SITE-A"),
	)
	mock.ExpectQuery("SELECT canonical_delivered_at").WithArgs("ORD-1001").WillReturnRows(
		sqlmock.NewRows([]string{"canonical_delivered_at"}).AddRow(time.Now().Add(-2*time.Hour)),
	)
	mock.ExpectQuery("SELECT returnable FROM order_lines").WithArgs("ORD-1001").WillReturnRows(
		sqlmock.NewRows([]string{"returnable"}).AddRow(true),
	)
	mock.ExpectQuery("SELECT timezone, business_start::text, business_end::text,").WithArgs("SITE-A").WillReturnRows(
		sqlmock.NewRows([]string{"timezone", "business_start", "business_end", "weekend_days", "holidays"}).AddRow("America/New_York", "08:00:00", "18:00:00", "0,6", ""),
	)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO support_tickets").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO user_assignments").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT id::text, returnable FROM order_lines WHERE order_id=\$1`).WithArgs("ORD-1001").WillReturnRows(
		sqlmock.NewRows([]string{"id", "returnable"}).AddRow("line-1", true),
	)
	mock.ExpectExec("INSERT INTO support_ticket_lines").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "11111111-1111-1111-1111-111111111111")
		c.Next()
	})
	r.POST("/tickets", h.CreateTicket)

	body := `{"order_id":"ORD-1001","ticket_type":"refund_only","priority":"STANDARD","description":"need refund"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id"`) {
		t.Fatalf("expected ticket id in response, got %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
