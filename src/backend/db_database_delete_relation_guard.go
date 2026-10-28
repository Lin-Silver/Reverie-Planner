package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// hasExternalActiveRelationProperty restricts database soft-deletion while
// another database owns an active relation definition targeting it. Include
// sources in trashed databases: restoring the source must not expose a broken
// relation. Internal relations can be restored with their own database.
// This must run in the same transaction as the database tombstone.
func hasExternalActiveRelationProperty(tx *sql.Tx, targetDatabaseID string) (bool, error) {
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
		var definition struct {
			TargetDatabaseID string `json:"targetDatabaseId"`
		}
		if err := json.Unmarshal([]byte(raw), &definition); err != nil || strings.TrimSpace(definition.TargetDatabaseID) == "" {
			return false, fmt.Errorf("invalid saved relation configuration")
		}
		if definition.TargetDatabaseID == targetDatabaseID {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
