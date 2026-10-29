package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

// Computed dependencies must be inspected inside the same transaction as a
// schema mutation, including uncommitted changes to referenced properties.
// These fixtures never touch a user's actual workspace or provider settings.
func TestComputedSchemaValidationUsesTransactionalDependencies(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()

    source, record := dbRegressionCreate(t, app)
    target, _ := dbRegressionCreate(t, app)
    number := computedProperty(t, app, source.ID, "Amount", "number", map[string]any{})
    price := computedProperty(t, app, target.ID, "Price", "number", map[string]any{})
    relation := computedProperty(t, app, source.ID, "Related", "relation", map[string]any{
        "targetDatabaseId": target.ID,
    })
    formulaConfig := fmt.Sprintf(`{"operation":"sum","args":[{"propertyId":%q}]}`, number.ID)
    rollupConfig := fmt.Sprintf(`{"relationPropertyId":%q,"targetPropertyId":%q,"aggregate":"sum"}`, relation.ID, price.ID)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    tx, err := app.DB.Begin()
    if err != nil { t.Fatal(err) }
    if _, err := tx.Exec("UPDATE db_properties SET deleted_at='2026-01-01' WHERE id=?", number.ID); err != nil {
        _ = tx.Rollback()
        t.Fatal(err)
    }
    if err := validateDBComputedDefinition(tx, source.ID, "formula", formulaConfig); err == nil {
        _ = tx.Rollback()
        t.Fatal("formula accepted an uncommitted deletion of its source property")
    }
    if err := tx.Rollback(); err != nil { t.Fatal(err) }
    if err := validateDBComputedDefinition(app.DB, source.ID, "formula", formulaConfig); err != nil {
        t.Fatalf("rolled-back formula source was not restored: %v", err)
    }

    tx, err = app.DB.Begin()
    if err != nil { t.Fatal(err) }
    if _, err := tx.Exec("UPDATE db_properties SET type='text' WHERE id=?", price.ID); err != nil {
        _ = tx.Rollback()
        t.Fatal(err)
    }
    if err := validateDBComputedDefinition(tx, source.ID, "rollup", rollupConfig); err == nil {
        _ = tx.Rollback()
        t.Fatal("numeric rollup accepted an uncommitted target type change")
    }
    if err := tx.Rollback(); err != nil { t.Fatal(err) }
    if err := validateDBComputedDefinition(app.DB, source.ID, "rollup", rollupConfig); err != nil {
        t.Fatalf("rolled-back rollup target was not restored: %v", err)
    }

    // Simulate an ambiguous relation definition saved by an older binary.
    // A rollup POST must fail closed without inserting a property or altering
    // the original record, and must recover after explicit fixture repair.
    ambiguous := fmt.Sprintf(`{"targetDatabaseId":%q,"targetDatabaseId":%q}`, target.ID, target.ID)
    if _, err := app.DB.Exec("UPDATE db_properties SET config=? WHERE id=?", ambiguous, relation.ID); err != nil {
        t.Fatal(err)
    }
    var beforeCount int
    if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", source.ID).Scan(&beforeCount); err != nil {
        t.Fatal(err)
    }
    request, err := json.Marshal(DBProperty{DatabaseID: source.ID, Name: "Must not persist", Type: "rollup", Config: rollupConfig})
    if err != nil { t.Fatal(err) }
    rejected := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(request), app.handleDBProperties)
    if rejected.Code != http.StatusBadRequest {
        t.Fatalf("ambiguous saved relation accepted by rollup POST: %d %s", rejected.Code, rejected.Body.String())
    }
    var afterCount, recordVersion int
    if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", source.ID).Scan(&afterCount); err != nil { t.Fatal(err) }
    if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", record.ID).Scan(&recordVersion); err != nil { t.Fatal(err) }
    if beforeCount != afterCount || recordVersion != record.Version {
        t.Fatalf("rejected rollup changed data: properties %d -> %d, record version %d -> %d", beforeCount, afterCount, record.Version, recordVersion)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)

    if _, err := app.DB.Exec("UPDATE db_properties SET config=? WHERE id=?", relation.Config, relation.ID); err != nil {
        t.Fatal(err)
    }
    accepted := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(request), app.handleDBProperties)
    if accepted.Code != http.StatusCreated {
        t.Fatalf("valid rollup rejected after repairing relation: %d %s", accepted.Code, accepted.Body.String())
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
