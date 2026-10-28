package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// hasIncomingDatabaseRelation restricts database deletion when a live source
// record in another database still contains a relation to the target database.
// A source database already in trash still counts, since restoring it would
// expose that live record. Trashed source records do not count: their restore
// path validates references, including deleted target databases. Empty
// relation properties also do not count, preserving existing trash semantics.
// Run this check inside the same transaction as the database soft deletion.
func hasIncomingDatabaseRelation(tx *sql.Tx, targetDatabaseID string) (bool, error) {
	rows, err := tx.Query(`SELECT p.config, v.value FROM db_values v
		JOIN db_properties p ON p.id=v.property_id
		JOIN db_records r ON r.id=v.record_id
		WHERE p.type='relation' AND p.deleted_at IS NULL
		  AND r.deleted_at IS NULL AND p.database_id<>?`, targetDatabaseID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var configRaw, valueRaw string
		if err := rows.Scan(&configRaw, &valueRaw); err != nil {
			return false, err
		}
		var config struct {
			TargetDatabaseID string `json:"targetDatabaseId"`
		}
		if err := json.Unmarshal([]byte(configRaw), &config); err != nil {
			return false, fmt.Errorf("invalid saved relation configuration: %w", err)
		}
		if strings.TrimSpace(config.TargetDatabaseID) == "" {
			return false, fmt.Errorf("invalid saved relation target")
		}
		if config.TargetDatabaseID != targetDatabaseID {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(valueRaw), &value); err != nil {
			return false, fmt.Errorf("invalid saved relation value: %w", err)
		}
		if value == nil {
			continue
		}
		ids, ok := value.([]any)
		if !ok || len(ids) > 500 {
			return false, fmt.Errorf("invalid saved relation value")
		}
		seen := make(map[string]struct{}, len(ids))
		for _, item := range ids {
			id, ok := item.(string)
			if !ok || strings.TrimSpace(id) == "" {
				return false, fmt.Errorf("invalid saved relation record ID")
			}
			if _, exists := seen[id]; exists {
				return false, fmt.Errorf("duplicate saved relation record ID")
			}
			seen[id] = struct{}{}
		}
		if len(ids) > 0 {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
