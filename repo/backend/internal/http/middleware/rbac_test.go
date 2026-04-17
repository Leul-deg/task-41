package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestRequirePermission_MissingActorReturns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, _ := sqlmock.New()
	defer db.Close()

	r := gin.New()
	r.GET("/test", RequirePermission(db, "hiring", "view"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRequirePermission_NotAllowedReturns403(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("user-1", "hiring", "view").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(CtxUserID, "user-1"); c.Next() })
	r.GET("/test", RequirePermission(db, "hiring", "view"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRequirePermission_AllowedProceedsToHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("user-1", "hiring", "view").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(CtxUserID, "user-1"); c.Next() })
	r.GET("/test", RequirePermission(db, "hiring", "view"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
