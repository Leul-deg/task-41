package repository

import "database/sql"

type AuditRepository struct {
	DB *sql.DB
}

func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{DB: db}
}

func (r *AuditRepository) Write(actorID, actionClass, entityType, entityID, eventJSON string) {
	_, _ = r.DB.Exec(`
		INSERT INTO audit_logs(actor_id, action_class, entity_type, entity_id, event_data)
		VALUES ($1,$2,$3,$4,$5::jsonb)
	`, actorID, actionClass, entityType, entityID, eventJSON)
}
