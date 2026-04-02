package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestComputeConfirmationStatus(t *testing.T) {
	status, released, err := ComputeConfirmationStatus(10, 6)
	if err != nil {
		t.Fatal(err)
	}
	if status != "PARTIAL_CONFIRMED" || released != 4 {
		t.Fatalf("unexpected result status=%s released=%d", status, released)
	}

	status, released, err = ComputeConfirmationStatus(10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || released != 0 {
		t.Fatalf("unexpected full confirmation result status=%s released=%d", status, released)
	}

	if _, _, err = ComputeConfirmationStatus(10, 11); err == nil {
		t.Fatal("expected error for invalid confirmation")
	}
}

func TestReleaseReservationsForOrderCancellation_ReleasesOnlyUnconfirmedQty(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	svc := NewInventoryService(db)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text").WithArgs("ORD-2002").WillReturnRows(
		sqlmock.NewRows([]string{"id"}).AddRow("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
	)
	mock.ExpectQuery("SELECT sku, warehouse_code, reserved_qty, confirmed_qty, released_qty").WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"sku", "warehouse_code", "reserved_qty", "confirmed_qty", "released_qty"}).AddRow("SKU-100", "WH-1", 10, 4, 0),
	)
	mock.ExpectExec("UPDATE inventory_reservations").WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 6).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE inventory_balances").WithArgs("WH-1", "SKU-100", 6).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO reservation_events").WithArgs(sqlmock.AnyArg(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 6, "ORDER_CANCELED").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	count, released, err := svc.ReleaseReservationsForOrderCancellation(context.Background(), "ORD-2002", "ORDER_CANCELED")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || released != 6 {
		t.Fatalf("expected count=1 released=6, got count=%d released=%d", count, released)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveSafetyStockThreshold_RoleConfiguredAndFallback(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	svc := NewInventoryService(db)

	mock.ExpectQuery("SELECT COALESCE").WithArgs("user-1", "SITE-A", "SKU-100").WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(12))
	threshold, err := svc.ResolveSafetyStockThreshold(context.Background(), "user-1", "SITE-A", "SKU-100")
	if err != nil {
		t.Fatal(err)
	}
	if threshold != 12 {
		t.Fatalf("expected threshold=12, got %d", threshold)
	}

	mock.ExpectQuery("SELECT COALESCE").WithArgs("user-1", "SITE-A", "SKU-999").WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(20))
	threshold, err = svc.ResolveSafetyStockThreshold(context.Background(), "user-1", "SITE-A", "SKU-999")
	if err != nil {
		t.Fatal(err)
	}
	if threshold != 20 {
		t.Fatalf("expected fallback threshold=20, got %d", threshold)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMoveStock_OutboundMissingInventoryRowReturnsClearError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	svc := NewInventoryService(db)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT on_hand, reserved FROM inventory_balances").WithArgs("WH-404", "SKU-404").WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := svc.MoveStock(context.Background(), "OUTBOUND", "SKU-404", "WH-404", "", "ORDER", 1, "user-1")
	if err == nil || !strings.Contains(err.Error(), "inventory row not found for warehouse+sku") {
		t.Fatalf("expected clear missing row error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
