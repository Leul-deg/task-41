package service

import (
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"meridian/backend/internal/domain/hiring"
	"meridian/backend/internal/platform/security"
)

func TestEvaluateBlocklist_PrioritizesBlockAndDuplicateRule(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	rows := sqlmock.NewRows([]string{"rule_type", "pattern", "severity"}).
		AddRow("domain", "example.com", "warn").
		AddRow("duplicate", "any", "block").
		AddRow("keyword", "test", "info")
	mock.ExpectQuery("SELECT rule_type, pattern, severity FROM blocklist_rules").WillReturnRows(rows)

	svc := NewHiringService(db, true, nil)
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

	svc := NewHiringService(db, false, nil)
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

	svc := NewHiringService(db, false, nil)
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

	svc := NewHiringService(db, false, nil)
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

func TestIdentityToken_IsDeterministicAndNotPlaintext(t *testing.T) {
	svc := NewHiringService(nil, false, security.NewPIIProtector(nil, "PII_TEST", "test-seed"))
	token1, err := svc.IdentityToken("person@example.com", "5551234567")
	if err != nil {
		t.Fatal(err)
	}
	token2, err := svc.IdentityToken("person@example.com", "5551234567")
	if err != nil {
		t.Fatal(err)
	}
	if token1 == "person@example.com|5551234567" {
		t.Fatalf("identity token should not equal raw identity")
	}
	if token1 != token2 {
		t.Fatalf("identity token should be deterministic")
	}
}

func TestImportCSV_EncryptsCandidateFieldsAndStoresTokenizedIdentity(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	svc := NewHiringService(db, false, security.NewPIIProtector(nil, "PII_TEST", "test-seed"))
	csvInput := "full_name,email,phone\nJane Doe,jane@example.com,555-111-2222\n"

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO candidates\(id, full_name, email, phone, name_search_token, created_at\)`).
		WithArgs(
			sqlmock.AnyArg(),
			nonPlaintextString{raw: "Jane Doe"},
			nonPlaintextString{raw: "jane@example.com"},
			nonPlaintextString{raw: "5551112222"},
			nonPlaintextString{raw: strings.ToLower("Jane")},
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO candidate_identities`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), nonPlaintextString{raw: "jane@example.com|5551112222"}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO applications`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "job-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO application_pipeline_events`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	created, err := svc.ImportCSV("job-1", strings.NewReader(csvInput))
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 created row, got %d", created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestScoreDuplicate_MatchesLegacyDeterministicToken(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	pii := security.NewPIIProtector(nil, "PII_TEST", "test-seed")
	svc := NewHiringService(db, false, pii)
	raw := "person@example.com|5551234567"
	versioned, err := pii.DeterministicTokens(raw)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := pii.LegacyDeterministicTokens(raw)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(`SELECT candidate_id FROM candidate_identities WHERE identity_key=\$1 LIMIT 1`).WithArgs(raw).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT candidate_id FROM candidate_identities WHERE identity_key=\$1 LIMIT 1`).WithArgs(security.HashOpaqueToken(raw)).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT candidate_id FROM candidate_identities WHERE identity_key=\$1 LIMIT 1`).WithArgs(versioned[0]).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT candidate_id FROM candidate_identities WHERE identity_key=\$1 LIMIT 1`).WithArgs(legacy[0]).WillReturnRows(
		sqlmock.NewRows([]string{"candidate_id"}).AddRow("candidate-1"),
	)

	risk, triggers, err := svc.ScoreDuplicate(raw, "Person Example")
	if err != nil {
		t.Fatal(err)
	}
	if risk < 90 {
		t.Fatalf("expected duplicate risk from legacy token, got %d", risk)
	}
	if len(triggers) == 0 || triggers[0] != "exact_identity" {
		t.Fatalf("expected exact_identity trigger, got %#v", triggers)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRemediateLegacyIdentityData_BackfillsPlaintextOnly(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	pii := security.NewPIIProtector(nil, "PII_TEST", "test-seed")
	svc := NewHiringService(db, false, pii)
	encName, err := pii.Encrypt("Jane Legacy")
	if err != nil {
		t.Fatal(err)
	}
	encEmail, err := pii.Encrypt("jane@example.com")
	if err != nil {
		t.Fatal(err)
	}
	encPhone, err := pii.Encrypt("5551112222")
	if err != nil {
		t.Fatal(err)
	}
	nameToken, err := svc.NameSearchToken("Jane Legacy")
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id::text, identity_key`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "identity_key"}).
			AddRow("11111111-1111-1111-1111-111111111111", "legacy@example.com|5551112222").
			AddRow("22222222-2222-2222-2222-222222222222", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
	)
	mock.ExpectExec(`UPDATE candidate_identities SET identity_key=\$2 WHERE id=\$1::uuid`).
		WithArgs("11111111-1111-1111-1111-111111111111", nonPlaintextString{raw: "legacy@example.com|5551112222"}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT id::text, full_name, COALESCE\(email,''\), COALESCE\(phone,''\), COALESCE\(name_search_token,''\)`).WillReturnRows(
		sqlmock.NewRows([]string{"id", "full_name", "email", "phone", "name_search_token"}).
			AddRow("33333333-3333-3333-3333-333333333333", encName, encEmail, encPhone, "").
			AddRow("44444444-4444-4444-4444-444444444444", "karen", "kevin@example.com", "5559990000", ""),
	)
	mock.ExpectExec(`UPDATE candidates`).
		WithArgs("33333333-3333-3333-3333-333333333333", encName, encEmail, encPhone, nameToken).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE candidates`).
		WithArgs(
			"44444444-4444-4444-4444-444444444444",
			nonPlaintextString{raw: "karen"},
			nonPlaintextString{raw: "kevin@example.com"},
			nonPlaintextString{raw: "5559990000"},
			nonPlaintextString{raw: strings.ToLower("kare")},
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	verifyName1, err := pii.Encrypt("Jane Legacy")
	if err != nil {
		t.Fatal(err)
	}
	verifyEmail1, err := pii.Encrypt("jane@example.com")
	if err != nil {
		t.Fatal(err)
	}
	verifyPhone1, err := pii.Encrypt("5551112222")
	if err != nil {
		t.Fatal(err)
	}
	verifyName2, err := pii.Encrypt("karen")
	if err != nil {
		t.Fatal(err)
	}
	verifyEmail2, err := pii.Encrypt("kevin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	verifyPhone2, err := pii.Encrypt("5559990000")
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`SELECT COALESCE\(full_name,''\), COALESCE\(email,''\), COALESCE\(phone,''\)`).WillReturnRows(
		sqlmock.NewRows([]string{"full_name", "email", "phone"}).
			AddRow(verifyName1, verifyEmail1, verifyPhone1).
			AddRow(verifyName2, verifyEmail2, verifyPhone2),
	)
	mock.ExpectCommit()

	if err := svc.RemediateLegacyIdentityData(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type nonPlaintextString struct {
	raw string
}

func (m nonPlaintextString) Match(v driver.Value) bool {
	s, ok := v.(string)
	return ok && s != "" && s != m.raw
}
