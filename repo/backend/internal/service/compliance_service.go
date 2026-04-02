package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"meridian/backend/internal/platform/masking"
)

type ComplianceService struct {
	DB *sql.DB
}

func NewComplianceService(db *sql.DB) *ComplianceService {
	return &ComplianceService{DB: db}
}

func (s *ComplianceService) RunCrawler() (int, int, error) {
	rows, err := s.DB.Query(`
		SELECT id::text, folder_path, min_interval_minutes, COALESCE(last_run_at, 'epoch'::timestamptz), nightly_cap, opt_out_marker
		FROM crawler_sources
		WHERE approved=true
		ORDER BY last_run_at NULLS FIRST
	`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	indexed := 0
	queued := 0
	globalCap := 5000

	for rows.Next() && indexed < globalCap {
		var sourceID, folder string
		var minInterval int
		var lastRun time.Time
		var capPerSource int
		var marker string
		if err := rows.Scan(&sourceID, &folder, &minInterval, &lastRun, &capPerSource, &marker); err != nil {
			continue
		}

		if time.Since(lastRun) < time.Duration(minInterval)*time.Minute {
			continue
		}

		files := []string{}
		_ = filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
					return filepath.SkipDir
				}
				return nil
			}
			files = append(files, path)
			return nil
		})

		sort.Strings(files)
		sourceProcessed := 0
		for _, f := range files {
			if indexed >= globalCap || sourceProcessed >= capPerSource {
				_, _ = s.DB.Exec(`INSERT INTO crawler_queue(source_id, file_path, status) VALUES ($1::uuid,$2,'PENDING') ON CONFLICT DO NOTHING`, sourceID, f)
				queued++
				continue
			}
			content, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			h := sha256.Sum256(content)
			excerpt := string(content)
			if len(excerpt) > 500 {
				excerpt = excerpt[:500]
			}
			excerpt = masking.MaskSSN(excerpt)
			_, _ = s.DB.Exec(`
				INSERT INTO searchable_index_entries(id, source_id, file_path, checksum, masked_excerpt)
				VALUES ($1,$2::uuid,$3,$4,$5)
				ON CONFLICT (file_path) DO UPDATE SET checksum=excluded.checksum, masked_excerpt=excluded.masked_excerpt, indexed_at=now()
			`, uuid.NewString(), sourceID, f, hex.EncodeToString(h[:]), sanitizeText(excerpt))
			indexed++
			sourceProcessed++
		}

		_, _ = s.DB.Exec(`UPDATE crawler_sources SET last_run_at=now() WHERE id=$1::uuid`, sourceID)
	}

	return indexed, queued, nil
}

func sanitizeText(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}
