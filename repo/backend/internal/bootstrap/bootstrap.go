package bootstrap

import (
	"database/sql"

	"github.com/google/uuid"
	"meridian/backend/internal/platform/security"
)

func SeedAdminAndKeys(db *sql.DB, username, password, clientKey, clientSecret, piiKeyName, piiKeyValue string, secretStore *security.SecretStoreProtector) error {
	passwordHash, err := security.HashPassword(password)
	if err != nil {
		return err
	}

	sealedClientSecret := clientSecret
	sealedPIIValue := piiKeyValue
	if secretStore != nil {
		sealedClientSecret, err = secretStore.EncryptIfNeeded(clientSecret)
		if err != nil {
			return err
		}
		sealedPIIValue, err = secretStore.EncryptIfNeeded(piiKeyValue)
		if err != nil {
			return err
		}
	}

	_, err = db.Exec(`
		INSERT INTO users(id, username, password_hash, site_code, warehouse_code, created_at)
		VALUES ($1,$2,$3,'SITE-A','WH-1',now())
		ON CONFLICT (username) DO NOTHING
	`, uuid.NewString(), username, passwordHash)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO client_keys(id, key_id, secret, created_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (key_id) DO NOTHING
	`, uuid.NewString(), clientKey, sealedClientSecret)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO encryption_keys(id, key_name, key_version, key_value, status, valid_from, created_at)
		VALUES ($1,$2,1,$3,'ACTIVE',now(),now())
		ON CONFLICT (key_name, key_version) DO NOTHING
	`, uuid.NewString(), piiKeyName, sealedPIIValue)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO roles(id, code, name) VALUES
		('11111111-1111-1111-1111-111111111111','ADMIN','System Administrator'),
		('22222222-2222-2222-2222-222222222222','HR_RECRUITER','HR Recruiter'),
		('33333333-3333-3333-3333-333333333333','HIRING_MANAGER','Hiring Manager'),
		('44444444-4444-4444-4444-444444444444','WAREHOUSE_CLERK','Warehouse Clerk'),
		('55555555-5555-5555-5555-555555555555','CS_AGENT','Customer Service Agent'),
		('66666666-6666-6666-6666-666666666666','COMPLIANCE_OFFICER','Compliance Officer')
		ON CONFLICT (code) DO NOTHING
	`)
	if err != nil {
		return err
	}

	permissionSeeds := []struct {
		Module string
		Action string
	}{
		{"hiring", "view"}, {"hiring", "create"}, {"hiring", "update"}, {"hiring", "approve"},
		{"support", "view"}, {"support", "create"}, {"support", "update"}, {"support", "approve"},
		{"inventory", "view"}, {"inventory", "create"}, {"inventory", "update"}, {"inventory", "approve"},
		{"admin", "view"}, {"admin", "update"}, {"admin", "delete"}, {"admin", "export"},
		{"compliance", "view"}, {"compliance", "create"}, {"compliance", "approve"}, {"compliance", "export"},
	}

	for _, p := range permissionSeeds {
		_, err = db.Exec(`
			INSERT INTO permissions(id, module, action)
			VALUES ($1,$2,$3)
			ON CONFLICT (module, action) DO NOTHING
		`, uuid.NewString(), p.Module, p.Action)
		if err != nil {
			return err
		}
	}

	_, err = db.Exec(`
		INSERT INTO role_permissions(id, role_id, permission_id)
		SELECT gen_random_uuid(), r.id, p.id
		FROM roles r CROSS JOIN permissions p
		WHERE r.code='ADMIN'
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO user_roles(id, user_id, role_id)
		SELECT gen_random_uuid(), u.id, r.id
		FROM users u JOIN roles r ON r.code='ADMIN'
		WHERE u.username=$1
		ON CONFLICT DO NOTHING
	`, username)
	if err != nil {
		return err
	}

	defaultUsers := []struct {
		Username string
		RoleCode string
		SiteCode string
		WHCode   string
	}{
		{"recruiter1", "HR_RECRUITER", "SITE-A", "WH-1"},
		{"manager1", "HIRING_MANAGER", "SITE-A", "WH-1"},
		{"clerk1", "WAREHOUSE_CLERK", "SITE-A", "WH-1"},
		{"agent1", "CS_AGENT", "SITE-A", "WH-1"},
		{"compliance1", "COMPLIANCE_OFFICER", "SITE-A", "WH-1"},
	}
	for _, u := range defaultUsers {
		uid := uuid.NewString()
		_, err = db.Exec(`
			INSERT INTO users(id, username, password_hash, site_code, warehouse_code, created_at)
			VALUES ($1,$2,$3,$4,$5,now())
			ON CONFLICT (username) DO NOTHING
		`, uid, u.Username, passwordHash, u.SiteCode, u.WHCode)
		if err != nil {
			return err
		}
		_, err = db.Exec(`
			INSERT INTO user_roles(id, user_id, role_id)
			SELECT gen_random_uuid(), us.id, r.id
			FROM users us JOIN roles r ON r.code=$2
			WHERE us.username=$1
			ON CONFLICT DO NOTHING
		`, u.Username, u.RoleCode)
		if err != nil {
			return err
		}
	}

	_, err = db.Exec(`
		INSERT INTO role_permissions(id, role_id, permission_id)
		SELECT gen_random_uuid(), r.id, p.id
		FROM roles r
		JOIN permissions p ON (
			(r.code='HR_RECRUITER' AND p.module='hiring' AND p.action IN ('view','create','update')) OR
			(r.code='HIRING_MANAGER' AND p.module='hiring' AND p.action IN ('view','approve')) OR
			(r.code='WAREHOUSE_CLERK' AND p.module='inventory' AND p.action IN ('view','create','update')) OR
			(r.code='CS_AGENT' AND p.module='support' AND p.action IN ('view','create','update','approve')) OR
			(r.code='COMPLIANCE_OFFICER' AND p.module='compliance' AND p.action IN ('view','create','approve','export'))
		)
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO scope_rules(id, role_id, module, scope, scope_value)
		SELECT gen_random_uuid(), r.id, m.module, m.scope, m.scope_value
		FROM roles r
		JOIN (
			VALUES
			('ADMIN','hiring','global',NULL),
			('ADMIN','support','global',NULL),
			('ADMIN','inventory','global',NULL),
			('ADMIN','admin','global',NULL),
			('ADMIN','compliance','global',NULL),
			('HR_RECRUITER','hiring','site','SITE-A'),
			('HIRING_MANAGER','hiring','assigned',NULL),
			('WAREHOUSE_CLERK','inventory','warehouse','WH-1'),
			('CS_AGENT','support','assigned',NULL),
			('COMPLIANCE_OFFICER','compliance','global',NULL)
		) AS m(role_code,module,scope,scope_value)
		ON r.code = m.role_code
		ON CONFLICT DO NOTHING
	`)

	return err
}

func HardenSecretStorage(db *sql.DB, secretStore *security.SecretStoreProtector) error {
	if db == nil || secretStore == nil {
		return nil
	}

	clientRows, err := db.Query(`SELECT key_id, secret FROM client_keys WHERE revoked_at IS NULL`)
	if err != nil {
		return err
	}
	defer clientRows.Close()

	type clientSecretRow struct {
		keyID  string
		secret string
	}
	clientSecrets := []clientSecretRow{}
	for clientRows.Next() {
		var row clientSecretRow
		if clientRows.Scan(&row.keyID, &row.secret) == nil {
			clientSecrets = append(clientSecrets, row)
		}
	}

	for _, row := range clientSecrets {
		sealed, err := secretStore.EncryptIfNeeded(row.secret)
		if err != nil {
			return err
		}
		if sealed == row.secret {
			continue
		}
		if _, err := db.Exec(`UPDATE client_keys SET secret=$2 WHERE key_id=$1`, row.keyID, sealed); err != nil {
			return err
		}
	}

	keyRows, err := db.Query(`SELECT key_name, key_version, key_value FROM encryption_keys`)
	if err != nil {
		return err
	}
	defer keyRows.Close()

	type encryptionKeyRow struct {
		keyName    string
		keyVersion int
		keyValue   string
	}
	keyValues := []encryptionKeyRow{}
	for keyRows.Next() {
		var row encryptionKeyRow
		if keyRows.Scan(&row.keyName, &row.keyVersion, &row.keyValue) == nil {
			keyValues = append(keyValues, row)
		}
	}

	for _, row := range keyValues {
		sealed, err := secretStore.EncryptIfNeeded(row.keyValue)
		if err != nil {
			return err
		}
		if sealed == row.keyValue {
			continue
		}
		if _, err := db.Exec(`
			UPDATE encryption_keys
			SET key_value=$3
			WHERE key_name=$1 AND key_version=$2
		`, row.keyName, row.keyVersion, sealed); err != nil {
			return err
		}
	}

	return nil
}
