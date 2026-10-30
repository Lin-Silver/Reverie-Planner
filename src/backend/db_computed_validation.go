package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Keep invalid saved dependencies distinguishable from storage failures. A
// restore conflict is recoverable by restoring its inputs; a SQL fault is not.
var errInvalidComputedDefinition = errors.New("invalid computed dependency")

func invalidComputedDefinition(message string) error {
	return fmt.Errorf("%w: %s", errInvalidComputedDefinition, message)
}

func computedReferenceError(err error, missing string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return invalidComputedDefinition(missing)
	}
	return err
}

// Computed fields may only refer to supported persisted properties. Validate
// inside the mutation transaction, including when reactivating saved schema.
func validateDBComputedDefinition(db dbComputedQueryer, databaseID, kind, config string) error {
	switch kind {
	case "formula":
		def, err := parseFormulaDefinition(config)
		if err != nil {
			return invalidComputedDefinition(err.Error())
		}
		for _, operand := range def.Args {
			if operand.PropertyID == "" {
				continue
			}
			var propertyType string
			if err := db.QueryRow("SELECT type FROM db_properties WHERE id=? AND database_id=? AND deleted_at IS NULL", operand.PropertyID, databaseID).Scan(&propertyType); err != nil {
				return computedReferenceError(err, "formula property not found; restore input properties first")
			}
			if !map[string]bool{"title": true, "text": true, "number": true, "checkbox": true}[propertyType] {
				return invalidComputedDefinition("unsupported formula operand property type")
			}
		}
	case "rollup":
		def, err := parseRollupDefinition(config)
		if err != nil {
			return invalidComputedDefinition(err.Error())
		}
		var relationKind, relationConfig string
		if err := db.QueryRow("SELECT type,config FROM db_properties WHERE id=? AND database_id=? AND deleted_at IS NULL", def.RelationPropertyID, databaseID).Scan(&relationKind, &relationConfig); err != nil {
			return computedReferenceError(err, "rollup relation not found; restore relation property first")
		}
		if relationKind != "relation" {
			return invalidComputedDefinition("rollup must reference relation property")
		}
		var relation struct {
			TargetDatabaseID string `json:"targetDatabaseId"`
		}
		if err := rejectDuplicateJSONKeys([]byte(relationConfig)); err != nil {
			return invalidComputedDefinition("invalid relation configuration")
		}
		if err := json.Unmarshal([]byte(relationConfig), &relation); err != nil || relation.TargetDatabaseID == "" {
			return invalidComputedDefinition("invalid relation configuration")
		}
		var targetType string
		if err := db.QueryRow(`SELECT p.type FROM db_properties p JOIN databases d ON d.id=p.database_id
			WHERE p.id=? AND p.database_id=? AND p.deleted_at IS NULL AND d.deleted_at IS NULL`, def.TargetPropertyID, relation.TargetDatabaseID).Scan(&targetType); err != nil {
			return computedReferenceError(err, "rollup target unavailable; restore target database and property first")
		}
		if def.Aggregate != "count" && targetType != "number" {
			return invalidComputedDefinition("numeric rollup requires a number property")
		}
	}
	return nil
}
