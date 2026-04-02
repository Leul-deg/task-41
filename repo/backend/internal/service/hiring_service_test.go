package service

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"meridian/backend/internal/domain/hiring"
)

func TestEvaluateBlocklist_PrioritizesBlockAndDuplicateRule(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	rows := sqlmock.NewRows([]string{"rule_type", "pattern", "severity"}).
		AddRow("domain", "example.com", "warn").
		AddRow("duplicate", "any", "block").
		AddRow("keyword", "test", "info")
	mock.ExpectQuery("SELECT rule_type, pattern, severity FROM blocklist_rules").WillReturnRows(rows)

	svc := NewHiringService(db, true)
	sev, triggers, err := svc.EvaluateBlocklist("person@example.com", "test user", true)
	if err != nil {
		t.Fatal(err)
	}
	if sev != hiring.SeverityBlock {
		t.Fatalf("expected block severity, got %s", sev)
	}
	if len(triggers) < 2 {
		t.Fatalf("expected triggers to include duplicate/domain, got %#v", triggers)
	}
}

func TestTransition_ReturnsStageMismatchBeforeTransitionLookup(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	svc := NewHiringService(db, false)
	appID := "11111111-1111-1111-1111-111111111111"

	mock.ExpectQuery("SELECT stage_code, pipeline_template_id::text").WithArgs(appID).WillReturnRows(
		sqlmock.NewRows([]string{"stage_code", "pipeline_template_id"}).AddRow("TEST", nil),
	)

	err := svc.Transition(hiring.TransitionInput{
		ApplicationID: appID,
		FromStage:     "screening",
		ToStage:       "invitation",
		Provided:      map[string]string{"notes": "ok"},
	}, "actor-1")
	if err == nil || !strings.Contains(err.Error(), "stage mismatch") {
		t.Fatalf("expected stage mismatch error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTransition_UsesFallbackMapAndNormalizedStages(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	svc := NewHiringService(db, false)
	appID := "22222222-2222-2222-2222-222222222222"
	actorID := "actor-2"

	mock.ExpectQuery("SELECT stage_code, pipeline_template_id::text").WithArgs(appID).WillReturnRows(
		sqlmock.NewRows([]string{"stage_code", "pipeline_template_id"}).AddRow("SCREENING", nil),
	)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE applications SET stage_code=\\$2").WithArgs(appID, "INVITATION").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO application_pipeline_events").WithArgs(
		sqlmock.AnyArg(),
		appID,
		actorID,
		"SCREENING",
		"INVITATION",
		`{"notes":"captured"}`,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := svc.Transition(hiring.TransitionInput{
		ApplicationID: appID,
		FromStage:     "screening",
		ToStage:       "invitation",
		Provided:      map[string]string{"notes": "captured"},
	}, actorID)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTransition_WithTemplateInvalidPairReturnsInvalidStageTransition(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	svc := NewHiringService(db, false)
	appID := "33333333-3333-3333-3333-333333333333"
	templateID := "44444444-4444-4444-4444-444444444444"

	mock.ExpectQuery("SELECT stage_code, pipeline_template_id::text").WithArgs(appID).WillReturnRows(
		sqlmock.NewRows([]string{"stage_code", "pipeline_template_id"}).AddRow("SCREENING", templateID),
	)
	mock.ExpectQuery(`SELECT COALESCE\(required_fields,''\)`).WithArgs(templateID, "SCREENING", "REJECT").WillReturnError(sql.ErrNoRows)

	err := svc.Transition(hiring.TransitionInput{
		ApplicationID: appID,
		FromStage:     "SCREENING",
		ToStage:       "REJECT",
		Provided:      map[string]string{},
	}, "actor-3")
	if err == nil || err.Error() != "invalid stage transition" {
		t.Fatalf("expected invalid stage transition, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
