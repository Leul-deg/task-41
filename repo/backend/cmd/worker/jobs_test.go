package main

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRunReservationReleases_ExecutesReleaseStatement(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id"}).AddRow("11111111-1111-1111-1111-111111111111")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text").WillReturnRows(rows)
	mock.ExpectQuery("SELECT sku, warehouse_code, reserved_qty, confirmed_qty, released_qty").WithArgs("11111111-1111-1111-1111-111111111111").WillReturnRows(
		sqlmock.NewRows([]string{"sku", "warehouse_code", "reserved_qty", "confirmed_qty", "released_qty"}).AddRow("SKU-100", "WH-1", 5, 0, 0),
	)
	mock.ExpectExec("UPDATE inventory_reservations").WithArgs("11111111-1111-1111-1111-111111111111", 5).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE inventory_balances").WithArgs("WH-1", "SKU-100", 5).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO reservation_events").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	runReservationReleases(context.Background(), db)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRunNightlyCrawler_ExecutesOncePerNight(t *testing.T) {
	s := NewNightlyCrawlerScheduler()
	now := time.Date(2026, 3, 30, 2, 5, 0, 0, time.UTC)

	called := 0
	runner := func(context.Context) (int, int, error) {
		called++
		return 12, 3, nil
	}

	if !runNightlyCrawler(context.Background(), now, s, runner) {
		t.Fatal("expected nightly crawler to run")
	}
	if called != 1 {
		t.Fatalf("expected runner called once, got %d", called)
	}

	if runNightlyCrawler(context.Background(), now.Add(30*time.Minute), s, runner) {
		t.Fatal("expected second same-day run to be skipped")
	}
	if called != 1 {
		t.Fatalf("expected runner to remain once, got %d", called)
	}
}

func TestRunRetentionJobs_IncludesFinancialAnonymization(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectExec("DELETE FROM applications a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE support_tickets").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE orders").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("DELETE FROM request_nonces").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM idempotency_records").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM step_up_tokens").WillReturnResult(sqlmock.NewResult(0, 0))

	runRetentionJobs(context.Background(), db)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
