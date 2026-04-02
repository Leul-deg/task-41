package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"meridian/backend/internal/service"
)

func runReservationReleases(ctx context.Context, db *sql.DB) {
	svc := service.NewInventoryService(db)
	count, released, err := svc.ReleaseExpiredReservations(ctx, "HOLD_EXPIRED_AUTO_RELEASE")
	if err != nil {
		log.Printf("reservation release job error: %v", err)
		return
	}
	if count > 0 {
		log.Printf("reservation release job updated rows=%d released_qty=%d", count, released)
	}
}

type NightlyCrawlerScheduler struct {
	LastRunAt time.Time
	HourUTC   int
}

func NewNightlyCrawlerScheduler() *NightlyCrawlerScheduler {
	return &NightlyCrawlerScheduler{HourUTC: 2}
}

func (s *NightlyCrawlerScheduler) due(now time.Time) bool {
	hour := s.HourUTC
	if hour < 0 || hour > 23 {
		hour = 2
	}
	now = now.UTC()
	if now.Hour() < hour {
		return false
	}
	if s.LastRunAt.IsZero() {
		return true
	}
	last := s.LastRunAt.UTC()
	return !(last.Year() == now.Year() && last.YearDay() == now.YearDay())
}

func runNightlyCrawler(ctx context.Context, now time.Time, scheduler *NightlyCrawlerScheduler, run func(context.Context) (int, int, error)) bool {
	if scheduler == nil {
		scheduler = NewNightlyCrawlerScheduler()
	}
	if !scheduler.due(now) {
		return false
	}

	indexed, queued, err := run(ctx)
	if err != nil {
		log.Printf("nightly crawler run error: %v", err)
		return false
	}
	scheduler.LastRunAt = now.UTC()
	log.Printf("nightly crawler run completed indexed=%d queued=%d", indexed, queued)
	return true
}

func runTicketEscalation(ctx context.Context, db *sql.DB) {
	res, err := db.ExecContext(ctx, `
		UPDATE support_tickets
		SET escalated=true, updated_at=now()
		WHERE escalated=false
		  AND assignee_id IS NULL
		  AND COALESCE(sla_due_at, created_at) <= now()
	`)
	if err != nil {
		log.Printf("ticket escalation job error: %v", err)
		return
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("ticket escalation job updated rows=%d", n)
	}
}

func runRetentionJobs(ctx context.Context, db *sql.DB) {
	res1, err := db.ExecContext(ctx, `
		DELETE FROM applications a
		USING candidate_rejections r
		WHERE a.candidate_id = r.candidate_id
		  AND r.final_rejected_at <= now() - interval '2 years'
	`)
	if err != nil {
		log.Printf("retention candidate purge error: %v", err)
	} else {
		n, _ := res1.RowsAffected()
		if n > 0 {
			log.Printf("retention job purged applications=%d", n)
		}
	}

	res2, err := db.ExecContext(ctx, `
		UPDATE support_tickets
		SET description='[ANONYMIZED BY RETENTION JOB]', updated_at=now()
		WHERE created_at <= now() - interval '7 years'
		  AND description <> '[ANONYMIZED BY RETENTION JOB]'
	`)
	if err != nil {
		log.Printf("retention support anonymization error: %v", err)
	} else {
		n, _ := res2.RowsAffected()
		if n > 0 {
			log.Printf("retention job anonymized support rows=%d", n)
		}
	}

	res3, err := db.ExecContext(ctx, `
		UPDATE orders
		SET customer_ref='ANONYMIZED_FINANCIAL_RECORD'
		WHERE created_at <= now() - interval '7 years'
		  AND customer_ref <> 'ANONYMIZED_FINANCIAL_RECORD'
	`)
	if err != nil {
		log.Printf("retention financial anonymization error: %v", err)
	} else {
		n, _ := res3.RowsAffected()
		if n > 0 {
			log.Printf("retention job anonymized financial rows=%d", n)
		}
	}

	_, _ = db.ExecContext(ctx, `DELETE FROM request_nonces WHERE seen_at <= now() - interval '2 minutes'`)
	_, _ = db.ExecContext(ctx, `DELETE FROM idempotency_records WHERE expires_at <= now()`)
	_, _ = db.ExecContext(ctx, `DELETE FROM step_up_tokens WHERE expires_at <= now()`)
}
