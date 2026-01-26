package store

import (
	"database/sql"

	"github.com/rs/zerolog/log"
)

// MigrateAccountsToSub2APIStyle migrates the accounts table to sub2api-style structure
func (s *Store) MigrateAccountsToSub2APIStyle() error {
	log.Info().Msg("starting accounts table migration to sub2api style")

	// Check if migration is needed
	var hasStatusColumn bool
	rows, err := s.db.Query("PRAGMA table_info(accounts)")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, dfltValue, pk sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == "status" {
			hasStatusColumn = true
			break
		}
	}

	if hasStatusColumn {
		log.Info().Msg("accounts table already migrated, skipping")
		return nil
	}

	// Add new columns using helper function (ignores if column exists)
	migrations := []struct {
		column     string
		definition string
	}{
		// Status management
		{"status", "TEXT DEFAULT 'active'"},
		{"schedulable", "INTEGER DEFAULT 1"},
		{"error_message", "TEXT DEFAULT ''"},

		// Time-based scheduling controls
		{"rate_limited_at", "DATETIME"},
		{"rate_limit_reset_at", "DATETIME"},
		{"overload_until", "DATETIME"},

		// Temporary unschedulable
		{"temp_unschedulable_until", "DATETIME"},
		{"temp_unschedulable_reason", "TEXT DEFAULT ''"},

		// Priority (only if not exists - might be from old schema)
		{"priority", "INTEGER DEFAULT 100"},
		{"max_concurrency", "INTEGER DEFAULT 1"},
	}

	for i, mig := range migrations {
		log.Info().Int("step", i+1).Str("column", mig.column).Msg("adding column if not exists")
		if err := s.addColumnIfNotExists("accounts", mig.column, mig.definition); err != nil {
			// Only log as warning, not fatal error (column might already exist)
			log.Warn().Err(err).Str("column", mig.column).Msg("column might already exist")
		}
	}

	// Migrate existing data: set status based on is_active
	updateQuery := `UPDATE accounts SET status = CASE
		WHEN is_active = 1 THEN 'active'
		ELSE 'disabled'
	END WHERE status = 'active'`

	if _, err := s.db.Exec(updateQuery); err != nil {
		log.Error().Err(err).Msg("failed to migrate existing account statuses")
		return err
	}

	log.Info().Msg("accounts table migration completed successfully")
	return nil
}
