package main

import (
    "database/sql"
    "encoding/json"
    "strings"
)

// validateRestoredRecordRelations prevents a trashed record from exposing
// references to deleted, missing or reassigned targets when it is restored.
// Only active relation properties matter: a deleted property's own restore
// path validates its saved values separately. Run inside the restore tx.
func validateRestoredRecordRelations(tx *sql.Tx, recordID, databaseID string) (bool, error) {
    rows, err := tx.Query(`SELECT p.config, v.value FROM db_values v
        JOIN db_properties p ON p.id=v.property_id
        WHERE v.record_id=? AND p.database_id=? AND p.type='relation'
          AND p.deleted_at IS NULL`, recordID, databaseID)
    if err != nil {
        return false, err
    }
    type reference struct {
        targetDatabaseID string
        recordID string
    }
    references := make([]reference, 0)
    for rows.Next() {
        var config, raw string
        if err := rows.Scan(&config, &raw); err != nil {
            rows.Close()
            return false, err
        }
        var definition struct {
            TargetDatabaseID string `json:"targetDatabaseId"`
        }
        if err := json.Unmarshal([]byte(config), &definition); err != nil ||
            strings.TrimSpace(definition.TargetDatabaseID) == "" {
            rows.Close()
            return false, nil
        }
        var value any
        if err := json.Unmarshal([]byte(raw), &value); err != nil {
            rows.Close()
            return false, nil
        }
        if value == nil {
            continue
        }
        ids, ok := value.([]any)
        if !ok || len(ids) > 500 {
            rows.Close()
            return false, nil
        }
        seen := make(map[string]struct{}, len(ids))
        for _, item := range ids {
            id, ok := item.(string)
            if !ok || strings.TrimSpace(id) == "" {
                rows.Close()
                return false, nil
            }
            if _, duplicate := seen[id]; duplicate {
                rows.Close()
                return false, nil
            }
            seen[id] = struct{}{}
            references = append(references, reference{definition.TargetDatabaseID, id})
        }
    }
    if err := rows.Err(); err != nil {
        rows.Close()
        return false, err
    }
    if err := rows.Close(); err != nil {
        return false, err
    }

    for _, ref := range references {
        var count int
        if err := tx.QueryRow(`SELECT COUNT(*) FROM db_records r
            JOIN databases d ON d.id=r.database_id
            WHERE r.id=? AND r.database_id=? AND r.deleted_at IS NULL
              AND d.deleted_at IS NULL`, ref.recordID, ref.targetDatabaseID).Scan(&count); err != nil {
            return false, err
        }
        if count != 1 {
            return false, nil
        }
    }
    return true, nil
}
