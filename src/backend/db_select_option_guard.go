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
        // The normal record validator permits free-form values when the option
        // list is empty. A schema edit must nevertheless refuse to remove all
        // choices while saved selections still depend on them.
        var definition struct { Options []string `json:"options"` }
        if err := json.Unmarshal([]byte(config), &definition); err != nil { return false, nil }
        if len(definition.Options) == 0 && value != nil {
            if kind == "select" && value.(string) != "" { return false, nil }
            if kind == "multi_select" && len(value.([]any)) != 0 { return false, nil }
        }
    }
    if err := rows.Err(); err != nil { return false, err }
    return true, nil
}
