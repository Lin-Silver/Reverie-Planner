package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// hasIncomingDatabaseRelation prevents hiding a database while another
// database still has an active relation property targeting it. Even an empty
// relation is a live schema dependency, and a relation in a trashed source
// database must remain valid if that source is restored later. Relations
// entirely within the database being deleted are safe to preserve in trash.
// Call this inside the same transaction as the database soft deletion.
func hasIncomingDatabaseRelation(tx *sql.Tx, targetDatabaseID string) (bool, error) {
	rows, err := tx.Query(`SELECT config FROM db_properties
		WHERE type='relation' AND deleted_at IS NULL AND database_id<>?`, targetDatabaseID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		var config struct {
			TargetDatabaseID string `json:"targetDatabaseId"`
		}
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return false, fmt.Errorf("invalid saved relation configuration: %w", err)
		}
		if strings.TrimSpace(config.TargetDatabaseID) == "" {
			return false, fmt.Errorf("invalid saved relation target")
		}
		if config.TargetDatabaseID == targetDatabaseID {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
