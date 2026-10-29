package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
)

// insertInitialRecordValues validates every requested property against the
// current database schema and persists it inside the record/page creation
// transaction. The caller rolls back the entire transaction on any failure.
// Keep these checks aligned with the versioned record PUT path.
func insertInitialRecordValues(tx *sql.Tx, recordID, databaseID string, values map[string]any, now string) (int, error) {
    if len(values) == 0 {
        return 0, nil
    }
    type definition struct {
        kind   string
        config string
    }
    definitions := make(map[string]definition)
    rows, err := tx.Query(`SELECT id, type, config FROM db_properties
        WHERE database_id=? AND deleted_at IS NULL`, databaseID)
    if err != nil {
        return http.StatusInternalServerError, fmt.Errorf("could not read property definitions: %w", err)
    }
    for rows.Next() {
        var id, kind, config string
        if err := rows.Scan(&id, &kind, &config); err != nil {
            rows.Close()
            return http.StatusInternalServerError, fmt.Errorf("could not read property definition: %w", err)
        }
        definitions[id] = definition{kind: kind, config: config}
    }
    if err := rows.Err(); err != nil {
        rows.Close()
        return http.StatusInternalServerError, fmt.Errorf("could not read property definitions: %w", err)
    }
    if err := rows.Close(); err != nil {
        return http.StatusInternalServerError, fmt.Errorf("could not close property definitions: %w", err)
    }

    for propertyID, value := range values {
        def, exists := definitions[propertyID]
        if !exists {
            return http.StatusBadRequest, fmt.Errorf("unknown property")
        }
        if def.kind == "title" {
            return http.StatusBadRequest, fmt.Errorf("title must be changed through record title")
        }
        if err := validatePropertyValue(def.kind, value); err != nil {
            return http.StatusBadRequest, err
        }
        if err := validateConfiguredPropertyValue(def.kind, def.config, value); err != nil {
            return http.StatusBadRequest, err
        }
        if err := validateDBReferenceValue(tx, def.kind, def.config, value); err != nil {
            return http.StatusBadRequest, err
        }
        if def.kind == "file" {
            if err := validateDBFileReferences(tx, recordID, value); err != nil {
                return http.StatusBadRequest, err
            }
        }
        encoded, err := json.Marshal(value)
        if err != nil {
            return http.StatusBadRequest, fmt.Errorf("invalid property value: %w", err)
        }
        if _, err := tx.Exec(`INSERT INTO db_values (record_id, property_id, value, version, updated_at)
            VALUES (?,?,?,1,?)`, recordID, propertyID, string(encoded), now); err != nil {
            return http.StatusInternalServerError, fmt.Errorf("could not save initial property value: %w", err)
        }
    }
    return 0, nil
}
