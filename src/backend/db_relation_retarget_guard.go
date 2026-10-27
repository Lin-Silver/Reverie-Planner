package main

import (
	"database/sql"
	"encoding/json"
)

// validateRelationTargetChange prevents a relation property from silently
// reinterpreting saved record IDs as belonging to a different database.
// Both live and soft-deleted records matter because the latter can be restored.
// An empty array or null can be safely retargeted; populated or malformed
// legacy values require an explicit data migration instead.
func validateRelationTargetChange(tx *sql.Tx, propertyID, oldConfig, newConfig string) (bool, error) {
	var oldDef, newDef struct {
		TargetDatabaseID string `json:"targetDatabaseId"`
	}
	if err := json.Unmarshal([]byte(oldConfig), &oldDef); err != nil {
		return false, nil
	}
	if err := json.Unmarshal([]byte(newConfig), &newDef); err != nil {
		return false, nil
	}
	if oldDef.TargetDatabaseID == newDef.TargetDatabaseID {
		return true, nil
	}
	rows, err := tx.Query("SELECT value FROM db_values WHERE property_id=?", propertyID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return false, nil
		}
		if value == nil {
			continue
		}
		ids, ok := value.([]any)
		if !ok || len(ids) != 0 {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return true, nil
}
