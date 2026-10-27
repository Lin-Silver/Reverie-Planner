package main

import (
    "database/sql"
    "encoding/json"
)

// validateSelectOptionChange checks every stored value, including values of
// trashed records. An option change must not make restored records invalid.
// SQL failures are distinct from incompatible/legacy values so callers can
// report a server error rather than a user-resolvable conflict.
func validateSelectOptionChange(tx *sql.Tx, propertyID, kind, config string) (bool, error) {
    rows, err := tx.Query("SELECT value FROM db_values WHERE property_id=?", propertyID)
    if err != nil { return false, err }
    defer rows.Close()
    for rows.Next() {
        var raw string
        if err := rows.Scan(&raw); err != nil { return false, err }
        var value any
        if err := json.Unmarshal([]byte(raw), &value); err != nil { return false, nil }
        if err := validatePropertyValue(kind, value); err != nil { return false, nil }
        if err := validateConfiguredPropertyValue(kind, config, value); err != nil { return false, nil }
    }
    if err := rows.Err(); err != nil { return false, err }
    return true, nil
}
