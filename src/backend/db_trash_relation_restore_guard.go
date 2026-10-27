package main

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// validateRestoredRelationValues checks the saved values of a deleted relation
// property before it becomes visible again. Soft-deleted source records must
// be included because their values survive and can be restored later.
// A semantic mismatch is a user-resolvable conflict; SQL failures are errors.
func validateRestoredRelationValues(tx *sql.Tx, propertyID, targetDatabaseID string) (bool, error) {
	rows, err := tx.Query("SELECT value FROM db_values WHERE property_id=?", propertyID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	referenced := make(map[string]struct{})
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
		if !ok || len(ids) > 500 {
			return false, nil
		}
		seen := make(map[string]struct{}, len(ids))
		for _, item := range ids {
			id, ok := item.(string)
			if !ok || strings.TrimSpace(id) == "" {
				return false, nil
			}
			if _, exists := seen[id]; exists {
				return false, nil
			}
			seen[id] = struct{}{}
			referenced[id] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}

	for id := range referenced {
		var count int
		if err := tx.QueryRow(
			"SELECT COUNT(*) FROM db_records r JOIN databases d ON d.id=r.database_id "+
				"WHERE r.id=? AND r.database_id=? AND r.deleted_at IS NULL AND d.deleted_at IS NULL",
			id, targetDatabaseID,
		).Scan(&count); err != nil {
			return false, err
		}
		if count != 1 {
			return false, nil
		}
	}
	return true, nil
}
