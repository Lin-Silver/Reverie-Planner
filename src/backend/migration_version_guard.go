package main

import (
	"database/sql"
	"errors"
	"fmt"
)

// Keep this in sync with the highest numbered migration in initializeDatabase.
// Refuse to open newer databases before running any application migration: an
// older binary cannot safely infer the schema or semantics of a future release.
const latestSupportedSchemaMigration = 10

func rejectUnsupportedSchemaMigrations(db *sql.DB) error {
	var version int
	err := db.QueryRow(
		"SELECT version FROM schema_migrations WHERE version > ? ORDER BY version LIMIT 1",
		latestSupportedSchemaMigration,
	).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect database schema migrations: %w", err)
	}
	return fmt.Errorf(
		"database schema version %d is newer than supported version %d; open with a compatible application before modifying this database",
		version, latestSupportedSchemaMigration,
	)
}
