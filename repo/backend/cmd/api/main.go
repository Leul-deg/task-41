package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"meridian/backend/internal/bootstrap"
	"meridian/backend/internal/config"
	"meridian/backend/internal/http/handlers"
	"meridian/backend/internal/http/middleware"
	"meridian/backend/internal/platform/db"
	"meridian/backend/internal/platform/security"
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

	migrationDir := filepath.Join(".", "migrations")
	if _, err := os.Stat(migrationDir); err != nil {
		migrationDir = filepath.Join("/app", "migrations")
	}
	if err := db.Migrate(context.Background(), database, migrationDir); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	secretStore := security.NewSecretStoreProtector(cfg.SecretMasterKey)

	if err := bootstrap.SeedAdminAndKeys(database, cfg.DefaultAdminUser, cfg.DefaultAdminPass, cfg.BootstrapClientKey, cfg.BootstrapClientSec, cfg.PIIKeyName, cfg.PIIKeyValue, secretStore); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}
	if err := bootstrap.HardenSecretStorage(database, secretStore); err != nil {
		log.Fatalf("secret hardening failed: %v", err)
	}

	tokens := security.NewTokenManager(cfg.JWTSecret)
	pii := security.NewPIIProtector(database, cfg.PIIKeyName, cfg.PIIKeyValue)
	if err := pii.EnsureBootstrapKey(); err != nil {
		log.Fatalf("pii key bootstrap failed: %v", err)
	}
	if err := pii.VerifyActiveKeyMaterial(); err != nil {
		log.Fatalf("pii key material verification failed: %v", err)
	}
	if err := service.NewHiringService(database, cfg.EnableFuzzyDedup, pii).RemediateLegacyIdentityData(); err != nil {
		log.Fatalf("legacy hiring identity remediation failed: %v", err)
	}
	authHandler := handlers.NewAuthHandler(database, tokens, pii, cfg.AccessTTL, cfg.RefreshTTL, cfg.StepUpTTL)
	hiringHandler := handlers.NewHiringHandler(database, cfg.EnableFuzzyDedup, pii)
	supportHandler := handlers.NewSupportHandler(database, pii)
	inventoryHandler := handlers.NewInventoryHandler(database)
	adminHandler := handlers.NewAdminHandler(database, secretStore)
	complianceHandler := handlers.NewComplianceHandler(database)

	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	r.POST("/auth/login", middleware.RequireSignedRequests(database), authHandler.Login)
	r.POST("/auth/refresh", middleware.RequireSignedRequests(database), authHandler.Refresh)
	r.GET("/kiosk/jobs",
		middleware.RequireSignedRequests(database),
		middleware.RequireKioskToken(cfg.KioskSubmitSecret),
		hiringHandler.ListPublicKioskJobs,
	)
	r.POST("/kiosk/applications",
		middleware.RequireSignedRequests(database),
		middleware.RequireKioskToken(cfg.KioskSubmitSecret),
		middleware.AuditWrites(database),
		hiringHandler.CreatePublicKioskApplication,
	)

	api := r.Group("/api")
	api.Use(middleware.RequireSignedRequests(database))
	api.Use(middleware.RequireAccessToken(tokens))
	api.Use(middleware.AuditWrites(database))
	api.POST("/auth/step-up", authHandler.StepUp)
	api.GET("/auth/me", authHandler.Me)
	api.Use(middleware.RequireIdempotency(database, cfg.IdempotencyTTL))

	hiring := api.Group("/hiring")
	hiring.GET("/jobs", middleware.RequirePermission(database, "hiring", "view"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.ListJobs)
	hiring.GET("/jobs/for-intake", middleware.RequirePermission(database, "hiring", "create"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.ListJobsForIntake)
	hiring.GET("/applications", middleware.RequirePermission(database, "hiring", "view"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.ListApplications)
	hiring.GET("/pipelines/templates", middleware.RequirePermission(database, "hiring", "view"), hiringHandler.ListPipelineTemplates)
	hiring.GET("/pipelines/templates/:id", middleware.RequirePermission(database, "hiring", "view"), hiringHandler.GetPipelineTemplate)
	hiring.POST("/jobs", middleware.RequirePermission(database, "hiring", "create"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.CreateJob)
	hiring.POST("/applications/manual", middleware.RequirePermission(database, "hiring", "create"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.CreateManualApplication)
	hiring.POST("/applications/kiosk", middleware.RequirePermission(database, "hiring", "create"), middleware.RequireScope(database, "hiring", "global", "site"), hiringHandler.CreateKioskApplication)
	hiring.POST("/applications/import-csv", middleware.RequirePermission(database, "hiring", "create"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.ImportCSV)
	hiring.POST("/pipelines/templates", middleware.RequirePermission(database, "hiring", "update"), hiringHandler.CreatePipelineTemplate)
	hiring.PUT("/pipelines/templates/:id", middleware.RequirePermission(database, "hiring", "update"), hiringHandler.UpdatePipelineTemplate)
	hiring.POST("/pipelines/validate", middleware.RequirePermission(database, "hiring", "update"), hiringHandler.ValidatePipeline)
	hiring.POST("/applications/:id/transition", middleware.RequirePermission(database, "hiring", "update"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.TransitionApplication)
	hiring.GET("/applications/:id/allowed-transitions", middleware.RequirePermission(database, "hiring", "view"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.GetAllowedTransitions)
	hiring.GET("/applications/:id/events", middleware.RequirePermission(database, "hiring", "view"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.GetPipelineEvents)
	hiring.GET("/candidates/:id", middleware.RequirePermission(database, "hiring", "view"), middleware.RequireScope(database, "hiring", "global", "site", "assigned"), hiringHandler.GetCandidate)
	hiring.POST("/blocklist/rules", middleware.RequirePermission(database, "hiring", "update"), hiringHandler.CreateBlocklistRule)

	support := api.Group("/support")
	support.GET("/orders", middleware.RequirePermission(database, "support", "view"), middleware.RequireScope(database, "support", "global", "site", "assigned"), supportHandler.ListOrders)
	support.GET("/orders/for-intake", middleware.RequirePermission(database, "support", "create"), middleware.RequireScope(database, "support", "global", "site", "assigned"), supportHandler.ListOrders)
	support.GET("/tickets", middleware.RequirePermission(database, "support", "view"), middleware.RequireScope(database, "support", "global", "site", "assigned"), supportHandler.ListTickets)
	support.POST("/tickets", middleware.RequirePermission(database, "support", "create"), middleware.RequireScope(database, "support", "global", "site", "assigned"), supportHandler.CreateTicket)
	support.GET("/tickets/:id", middleware.RequirePermission(database, "support", "view"), middleware.RequireScope(database, "support", "global", "site", "assigned"), supportHandler.GetTicket)
	support.PUT("/tickets/:id", middleware.RequirePermission(database, "support", "update"), middleware.RequireScope(database, "support", "global", "site", "assigned"), supportHandler.UpdateTicket)
	support.POST("/tickets/:id/attachments", middleware.RequirePermission(database, "support", "update"), middleware.RequireScope(database, "support", "global", "site", "assigned"), supportHandler.AddAttachment)
	support.POST("/tickets/:id/conflict-resolve", middleware.RequirePermission(database, "support", "update"), middleware.RequireScope(database, "support", "global", "site", "assigned"), supportHandler.ResolveConflict)
	support.POST("/tickets/refund-approve",
		middleware.RequirePermission(database, "support", "approve"),
		middleware.RequireScope(database, "support", "global", "site", "assigned"),
		middleware.RequireStepUp(tokens, "refund_approval"),
		supportHandler.ApproveRefund,
	)

	inventory := api.Group("/inventory")
	inventory.GET("/orders", middleware.RequirePermission(database, "inventory", "view"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.ListOrders)
	inventory.GET("/orders/for-intake", middleware.RequirePermission(database, "inventory", "create"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.ListOrders)
	inventory.GET("/balances", middleware.RequirePermission(database, "inventory", "view"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.GetBalances)
	inventory.GET("/reservations", middleware.RequirePermission(database, "inventory", "view"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.ListReservations)
	inventory.POST("/inbound", middleware.RequirePermission(database, "inventory", "create"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.MoveInbound)
	inventory.POST("/outbound", middleware.RequirePermission(database, "inventory", "create"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.MoveOutbound)
	inventory.POST("/transfers", middleware.RequirePermission(database, "inventory", "create"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.MoveTransfer)
	inventory.POST("/cycle-counts", middleware.RequirePermission(database, "inventory", "create"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.CycleCount)
	inventory.POST("/reservations/order-create", middleware.RequirePermission(database, "inventory", "create"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.CreateReservation)
	inventory.POST("/reservations/order-cancel", middleware.RequirePermission(database, "inventory", "update"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.CancelOrderReservations)
	inventory.POST("/reservations/:id/confirm", middleware.RequirePermission(database, "inventory", "update"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.ConfirmReservation)
	inventory.POST("/reservations/:id/release", middleware.RequirePermission(database, "inventory", "update"), middleware.RequireScope(database, "inventory", "global", "site", "warehouse"), inventoryHandler.ReleaseReservation)
	inventory.POST("/ledger/:id/reverse",
		middleware.RequirePermission(database, "inventory", "approve"),
		middleware.RequireScope(database, "inventory", "global", "site", "warehouse"),
		middleware.RequireStepUp(tokens, "delete_or_reversal"),
		inventoryHandler.ReverseLedger,
	)

	admin := api.Group("/admin")
	admin.GET("/roles", middleware.RequirePermission(database, "admin", "view"), adminHandler.ListRoles)
	admin.PUT("/roles/:id/permissions",
		middleware.RequirePermission(database, "admin", "update"),
		middleware.RequireStepUp(tokens, "role_permission_change"),
		adminHandler.UpdateRolePermissions,
	)
	admin.PUT("/roles/:id/scopes",
		middleware.RequirePermission(database, "admin", "update"),
		middleware.RequireStepUp(tokens, "role_permission_change"),
		adminHandler.UpdateRoleScopes,
	)
	admin.POST("/client-keys/rotate",
		middleware.RequirePermission(database, "admin", "update"),
		middleware.RequireStepUp(tokens, "role_permission_change"),
		adminHandler.RotateClientKey,
	)
	admin.POST("/client-keys/:id/revoke",
		middleware.RequirePermission(database, "admin", "delete"),
		middleware.RequireStepUp(tokens, "delete_or_reversal"),
		adminHandler.RevokeClientKey,
	)

	compliance := api.Group("/compliance")
	compliance.POST("/crawler/run", middleware.RequirePermission(database, "compliance", "create"), middleware.RequireScope(database, "compliance", "global"), complianceHandler.RunCrawler)
	compliance.GET("/crawler/status", middleware.RequirePermission(database, "compliance", "view"), middleware.RequireScope(database, "compliance", "global"), complianceHandler.CrawlerStatus)
	compliance.POST("/deletion-requests", middleware.RequirePermission(database, "compliance", "create"), middleware.RequireScope(database, "compliance", "global"), complianceHandler.CreateDeletionRequest)
	compliance.GET("/deletion-requests", middleware.RequirePermission(database, "compliance", "view"), middleware.RequireScope(database, "compliance", "global"), complianceHandler.ListDeletionRequests)
	compliance.POST("/deletion-requests/:id/process",
		middleware.RequirePermission(database, "compliance", "approve"),
		middleware.RequireScope(database, "compliance", "global"),
		middleware.RequireStepUp(tokens, "delete_or_reversal"),
		complianceHandler.ProcessDeletionRequest,
	)
	compliance.GET("/retention/jobs", middleware.RequirePermission(database, "compliance", "view"), middleware.RequireScope(database, "compliance", "global"), complianceHandler.RetentionStatus)
	compliance.GET("/audit-logs", middleware.RequirePermission(database, "compliance", "view"), middleware.RequireScope(database, "compliance", "global"), complianceHandler.AuditLogs)
	compliance.GET("/audit-logs/export",
		middleware.RequirePermission(database, "compliance", "export"),
		middleware.RequireScope(database, "compliance", "global"),
		middleware.RequireStepUp(tokens, "export"),
		complianceHandler.ExportAuditLogs,
	)

	log.Printf("backend-api listening on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
