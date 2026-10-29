package main

import (
	"database/sql"
	"fmt"
)

// hasActiveComputedPropertyDependency prevents schema deletion from silently
// invalidating an active formula or rollup. Deleted databases are included:
// restoring their existing properties must not reveal broken dependencies.
// Property IDs are globally unique, so a rollup target can be checked across
// database boundaries without interpreting a user-controlled SQL identifier.
func hasActiveComputedPropertyDependency(tx *sql.Tx, propertyID, databaseID string) (bool, error) {
	rows, err := tx.Query(`SELECT database_id, type, config FROM db_properties
		WHERE deleted_at IS NULL AND id<>? AND type IN ('formula','rollup')`, propertyID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var ownerID, kind, config string
		if err := rows.Scan(&ownerID, &kind, &config); err != nil {
			return false, err
		}
		switch kind {
		case "formula":
			// Formulas can only depend on properties in their own database.
			if ownerID != databaseID {
				continue
			}
			def, err := parseFormulaDefinition(config)
			if err != nil {
				return false, fmt.Errorf("invalid saved formula configuration: %w", err)
			}
			for _, operand := range def.Args {
				if operand.PropertyID == propertyID {
					return true, nil
				}
			}
		case "rollup":
			def, err := parseRollupDefinition(config)
			if err != nil {
				return false, fmt.Errorf("invalid saved rollup configuration: %w", err)
			}
			if (ownerID == databaseID && def.RelationPropertyID == propertyID) ||
				def.TargetPropertyID == propertyID {
				return true, nil
			}
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
