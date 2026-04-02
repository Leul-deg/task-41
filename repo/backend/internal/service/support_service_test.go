package service

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestComputeSLADue_UsesConfiguredWeekendAndHolidays(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery("SELECT timezone, business_start::text, business_end::text,").
		WithArgs("SITE-A").
		WillReturnRows(sqlmock.NewRows([]string{"timezone", "business_start", "business_end", "weekend_days", "holidays"}).
			AddRow("America/New_York", "08:00:00", "18:00:00", "0,6", "2026-03-30"))

	svc := NewSupportService(db)
	start := time.Date(2026, 3, 27, 16, 0, 0, 0, time.UTC)
	due, err := svc.ComputeSLADue("SITE-A", "STANDARD", start)
	if err != nil {
		t.Fatal(err)
	}

	loc, _ := time.LoadLocation("America/New_York")
	local := due.In(loc)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		t.Fatalf("due should skip weekend, got %s", local)
	}
	if local.Format("2006-01-02") == "2026-03-30" {
		t.Fatalf("due should skip holiday 2026-03-30, got %s", local)
	}
}
