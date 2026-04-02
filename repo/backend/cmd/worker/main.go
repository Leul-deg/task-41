package main

import (
	"context"
	"log"
	"time"

	"meridian/backend/internal/config"
	"meridian/backend/internal/platform/db"
	"meridian/backend/internal/service"
)

func main() {
	cfg := config.Load()
	if err := cfg.ValidateSecurity(); err != nil {
		log.Fatalf("invalid secure configuration: %v", err)
	}
	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open failed: %v", err)
	}
	defer database.Close()

	log.Printf("backend-worker started")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	crawlerScheduler := NewNightlyCrawlerScheduler()
	complianceSvc := service.NewComplianceService(database)

	for {
		select {
		case <-ticker.C:
			now := time.Now().UTC()
			runNightlyCrawler(context.Background(), now, crawlerScheduler, func(context.Context) (int, int, error) {
				return complianceSvc.RunCrawler()
			})
			runReservationReleases(context.Background(), database)
			runTicketEscalation(context.Background(), database)
			runRetentionJobs(context.Background(), database)
		}
	}
}
