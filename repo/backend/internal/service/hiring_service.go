package service

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"
	"meridian/backend/internal/domain/hiring"
)

type HiringService struct {
	DB               *sql.DB
	EnableFuzzyDedup bool
}

func NewHiringService(db *sql.DB, enableFuzzy bool) *HiringService {
	return &HiringService{DB: db, EnableFuzzyDedup: enableFuzzy}
}

func (s *HiringService) ValidateDefinition(stages []map[string]any, transitions []map[string]any) error {
	if len(stages) < 3 || len(stages) > 20 {
		return errors.New("stage count must be between 3 and 20")
	}

	seen := map[string]bool{}
	okSuccess := false
	okFailure := false

	for _, st := range stages {
		sc := strings.ToUpper(strings.TrimSpace(fmt.Sprint(st["code"])))
		if sc == "" {
			return errors.New("stage code required")
		}
		if seen[sc] {
			return fmt.Errorf("duplicate stage code %s", sc)
		}
		seen[sc] = true
		if toBool(st["terminal"]) {
			outcome := strings.ToLower(strings.TrimSpace(fmt.Sprint(st["outcome"])))
			if outcome == "success" {
				okSuccess = true
			}
			if outcome == "failure" {
				okFailure = true
			}
		}
	}
	if !okSuccess || !okFailure {
		return errors.New("pipeline requires at least one success and one failure terminal stage")
	}

	for _, tr := range transitions {
		from := strings.ToUpper(strings.TrimSpace(fmt.Sprint(tr["from_stage_code"])))
		to := strings.ToUpper(strings.TrimSpace(fmt.Sprint(tr["to_stage_code"])))
		if from == "" || to == "" {
			return errors.New("transition must have from_stage_code and to_stage_code")
		}
		if !seen[from] || !seen[to] {
			return fmt.Errorf("transition references unknown stage %s -> %s", from, to)
		}
	}

	return nil
}

func (s *HiringService) ValidateAndSavePipeline(code, name string, stages []map[string]any, transitions []map[string]any) (string, error) {
	if err := s.ValidateDefinition(stages, transitions); err != nil {
		return "", err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	tplID := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO pipeline_templates(id, code, name) VALUES ($1,$2,$3)`, tplID, code, name); err != nil {
		return "", err
	}

	sort.Slice(stages, func(i, j int) bool {
		return toInt(stages[i]["order_index"]) < toInt(stages[j]["order_index"])
	})

	for _, st := range stages {
		_, err := tx.Exec(`
			INSERT INTO pipeline_stages(id, template_id, code, name, order_index, terminal, outcome, required_fields)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		`, uuid.NewString(), tplID,
			strings.ToUpper(strings.TrimSpace(fmt.Sprint(st["code"]))),
			fmt.Sprint(st["name"]),
			toInt(st["order_index"]),
			toBool(st["terminal"]),
			strings.ToLower(strings.TrimSpace(fmt.Sprint(st["outcome"]))),
			fmt.Sprint(st["required_fields"]),
		)
		if err != nil {
			return "", err
		}
	}

	for _, tr := range transitions {
		_, err := tx.Exec(`
			INSERT INTO pipeline_transitions(id, template_id, from_stage_code, to_stage_code, required_fields, screening_rule)
			VALUES ($1,$2,$3,$4,$5,$6)
		`, uuid.NewString(), tplID,
			strings.ToUpper(strings.TrimSpace(fmt.Sprint(tr["from_stage_code"]))),
			strings.ToUpper(strings.TrimSpace(fmt.Sprint(tr["to_stage_code"]))),
			fmt.Sprint(tr["required_fields"]),
			fmt.Sprint(tr["screening_rule"]),
		)
		if err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return tplID, nil
}

func (s *HiringService) EvaluateBlocklist(email, fullName string, hasDuplicate bool) (hiring.BlockSeverity, []string, error) {
	severity := hiring.SeverityInfo
	triggers := []string{}

	rows, err := s.DB.Query(`SELECT rule_type, pattern, severity FROM blocklist_rules WHERE active=true`)
	if err != nil {
		return severity, triggers, nil
	}
	defer rows.Close()

	emailLC := strings.ToLower(strings.TrimSpace(email))
	nameLC := strings.ToLower(strings.TrimSpace(fullName))

	for rows.Next() {
		var rt, p, sev string
		if err := rows.Scan(&rt, &p, &sev); err != nil {
			return severity, triggers, err
		}
		match := false
		switch strings.ToLower(rt) {
		case "domain":
			parts := strings.Split(emailLC, "@")
			if len(parts) == 2 && strings.EqualFold(parts[1], strings.ToLower(strings.TrimSpace(p))) {
				match = true
			}
		case "keyword":
			if strings.Contains(nameLC, strings.ToLower(strings.TrimSpace(p))) {
				match = true
			}
		case "duplicate":
			if hasDuplicate {
				pattern := strings.ToLower(strings.TrimSpace(p))
				if pattern == "" || pattern == "any" || pattern == "identity" {
					match = true
				}
			}
		}
		if match {
			triggers = append(triggers, rt+":"+p)
			severity = maxSeverity(severity, hiring.BlockSeverity(strings.ToLower(sev)))
		}
	}

	return severity, triggers, nil
}

func (s *HiringService) ScoreDuplicate(identityKey, name string) (int, []string, error) {
	risk := 0
	triggers := []string{}
	var existingID string
	err := s.DB.QueryRow(`SELECT candidate_id FROM candidate_identities WHERE identity_key=$1 LIMIT 1`, identityKey).Scan(&existingID)
	if err == nil {
		risk += 90
		triggers = append(triggers, "exact_identity")
	}

	if s.EnableFuzzyDedup {
		var count int
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM candidates WHERE lower(full_name) LIKE $1`, strings.ToLower(prefix(name))+"%").Scan(&count)
		if count > 0 {
			risk += 15
			triggers = append(triggers, "fuzzy_name_prefix")
		}
	}

	if risk > 100 {
		risk = 100
	}
	return risk, triggers, nil
}

func (s *HiringService) ImportCSV(jobID string, r io.Reader) (int, error) {
	csvr := csv.NewReader(r)
	rows, err := csvr.ReadAll()
	if err != nil {
		return 0, err
	}
	if len(rows) < 2 {
		return 0, errors.New("csv must contain header and at least one row")
	}

	created := 0
	for i := 1; i < len(rows); i++ {
		if len(rows[i]) < 3 {
			continue
		}
		fullName := rows[i][0]
		email := strings.ToLower(strings.TrimSpace(rows[i][1]))
		phone := onlyDigits(rows[i][2])
		candidateID := uuid.NewString()
		appID := uuid.NewString()
		identity := email + "|" + phone

		tx, err := s.DB.Begin()
		if err != nil {
			return created, err
		}
		_, err = tx.Exec(`INSERT INTO candidates(id, full_name, email, phone, created_at) VALUES ($1,$2,$3,$4,now())`, candidateID, fullName, email, phone)
		if err != nil {
			tx.Rollback()
			continue
		}
		_, err = tx.Exec(`INSERT INTO candidate_identities(id, candidate_id, identity_key, created_at) VALUES ($1,$2,$3,now())`, uuid.NewString(), candidateID, identity)
		if err != nil {
			tx.Rollback()
			continue
		}
		_, err = tx.Exec(`INSERT INTO applications(id, candidate_id, job_id, source_type, stage_code, created_at) VALUES ($1,$2,$3,'CSV','SCREENING',now())`, appID, candidateID, jobID)
		if err != nil {
			tx.Rollback()
			continue
		}
		_, _ = tx.Exec(`INSERT INTO application_pipeline_events(id, application_id, to_stage_code, event_type) VALUES ($1,$2,'SCREENING','CREATE')`, uuid.NewString(), appID)
		if err := tx.Commit(); err != nil {
			continue
		}
		created++
	}

	return created, nil
}

func (s *HiringService) Transition(input hiring.TransitionInput, actorID string) error {
	fromStage := strings.ToUpper(strings.TrimSpace(input.FromStage))
	toStage := strings.ToUpper(strings.TrimSpace(input.ToStage))

	var currentStage string
	var templateID sql.NullString
	err := s.DB.QueryRow(`
		SELECT stage_code, pipeline_template_id::text
		FROM applications
		WHERE id=$1
	`, input.ApplicationID).Scan(&currentStage, &templateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("application not found")
		}
		return err
	}

	current := strings.ToUpper(strings.TrimSpace(currentStage))
	if current != fromStage {
		return fmt.Errorf("stage mismatch: current stage is %s, expected %s", current, fromStage)
	}

	required := ""
	if templateID.Valid && strings.TrimSpace(templateID.String) != "" {
		err = s.DB.QueryRow(`
			SELECT COALESCE(required_fields,'')
			FROM pipeline_transitions
			WHERE template_id=$1::uuid AND from_stage_code=$2 AND to_stage_code=$3
		`, templateID.String, fromStage, toStage).Scan(&required)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("invalid stage transition")
			}
			return err
		}
	} else {
		fallback := map[string]map[string]string{
			"SCREENING":  {"INVITATION": "notes"},
			"INVITATION": {"TEST": "invitation_date"},
			"TEST":       {"INTERVIEW": "test_score"},
			"INTERVIEW":  {"OFFER": "interview_result", "REJECT": "reject_reason"},
			"OFFER":      {"HIRE": "offer_accept_date", "REJECT": "reject_reason"},
		}
		stageTransitions, ok := fallback[fromStage]
		if !ok {
			return errors.New("invalid stage transition")
		}
		var found bool
		required, found = stageTransitions[toStage]
		if !found {
			return errors.New("invalid stage transition")
		}
	}

	if required != "" {
		for _, f := range strings.Split(required, ",") {
			k := strings.TrimSpace(f)
			if k == "" {
				continue
			}
			if strings.TrimSpace(input.Provided[k]) == "" {
				return fmt.Errorf("required field missing: %s", k)
			}
		}
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE applications SET stage_code=$2, updated_at=now() WHERE id=$1`, input.ApplicationID, toStage)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO application_pipeline_events(id, application_id, actor_id, from_stage_code, to_stage_code, event_type, details)
		VALUES ($1,$2,$3,$4,$5,'TRANSITION',$6::jsonb)
	`, uuid.NewString(), input.ApplicationID, actorID, fromStage, toStage, toJSON(input.Provided))
	if err != nil {
		return err
	}

	return tx.Commit()
}

func maxSeverity(a, b hiring.BlockSeverity) hiring.BlockSeverity {
	r := map[hiring.BlockSeverity]int{hiring.SeverityInfo: 1, hiring.SeverityWarn: 2, hiring.SeverityBlock: 3}
	if r[b] > r[a] {
		return b
	}
	return a
}

func toBool(v any) bool {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
	return s == "true" || s == "1" || s == "yes"
}

func toInt(v any) int {
	var x int
	fmt.Sscan(fmt.Sprint(v), &x)
	return x
}

func prefix(s string) string {
	x := strings.TrimSpace(s)
	if len(x) > 4 {
		return x[:4]
	}
	return x
}

func onlyDigits(in string) string {
	b := strings.Builder{}
	for _, r := range in {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toJSON(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("\"%s\":\"%s\"", escape(k), escape(v)))
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ",") + "}"
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}
