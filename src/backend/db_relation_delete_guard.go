package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "strings"
)

// hasIncomingActiveRelation enforces restrict-on-delete semantics for active
// relation properties and active source records. Sources in trashed databases
// still count: restoring that database would otherwise expose a dangling link.
// A trashed source record is excluded because its restore path validates links.
// The source being deleted is excluded so a self-relation cannot block itself.
// Run the check and the soft delete in the same SQLite transaction.
func hasIncomingActiveRelation(tx *sql.Tx, targetID, targetDatabaseID string) (bool, error) {
    rows, err := tx.Query(`SELECT p.config, v.value FROM db_values v
        JOIN db_properties p ON p.id=v.property_id
        JOIN db_records r ON r.id=v.record_id
        WHERE p.type='relation' AND p.deleted_at IS NULL
          AND r.deleted_at IS NULL AND r.id<>?`, targetID)
    if err != nil { return false, err }
    defer rows.Close()
    for rows.Next() {
        var config, raw string
        if err := rows.Scan(&config, &raw); err != nil { return false, err }
        // Do not trust last-wins JSON parsing for persisted relation targets.
        if err := rejectDuplicateJSONKeys([]byte(config)); err != nil {
            return false, fmt.Errorf("invalid saved relation configuration: %w", err)
        }
        var definition struct {
            TargetDatabaseID string `json:"targetDatabaseId"`
        }
        if err := json.Unmarshal([]byte(config), &definition); err != nil ||
            strings.TrimSpace(definition.TargetDatabaseID)=="" {
            return false, fmt.Errorf("invalid saved relation configuration")
        }
        if definition.TargetDatabaseID != targetDatabaseID { continue }
        var value any
        if err := json.Unmarshal([]byte(raw), &value); err != nil {
            return false, fmt.Errorf("invalid saved relation value: %w", err)
        }
        if value == nil { continue }
        ids, ok := value.([]any)
        if !ok || len(ids)>500 { return false, fmt.Errorf("invalid saved relation value") }
        seen := make(map[string]struct{}, len(ids))
        for _, item := range ids {
            recordID, ok := item.(string)
            if !ok || strings.TrimSpace(recordID)=="" {
                return false, fmt.Errorf("invalid saved relation record ID")
            }
            if _, exists := seen[recordID]; exists {
                return false, fmt.Errorf("duplicate saved relation record ID")
            }
            seen[recordID] = struct{}{}
            if recordID == targetID { return true, nil }
        }
    }
    if err := rows.Err(); err != nil { return false, err }
    return false, nil
}
