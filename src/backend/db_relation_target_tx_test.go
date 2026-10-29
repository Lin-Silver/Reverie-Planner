package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

// Relation target existence must be checked using the schema-write transaction,
// not a snapshot obtained before that transaction starts.
func TestRelationTargetValidationSeesUncommittedTombstone(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    _, _ = dbRegressionCreate(t, app)
    target, _ := dbRegressionCreate(t, app)
    config := fmt.Sprintf(`{"targetDatabaseId":%q}`, target.ID)

    tx, err := app.DB.Begin()
    if err != nil { t.Fatal(err) }
    if _, err := tx.Exec("UPDATE databases SET deleted_at='2026-01-01' WHERE id=?", target.ID); err != nil {
        _ = tx.Rollback()
        t.Fatal(err)
    }
    active, err := relationTargetActiveInTx(tx, config)
    if err != nil { _ = tx.Rollback(); t.Fatal(err) }
    if active { _ = tx.Rollback(); t.Fatal("relation accepted an uncommitted target tombstone") }
    if err := tx.Rollback(); err != nil { t.Fatal(err) }

    tx, err = app.DB.Begin()
    if err != nil { t.Fatal(err) }
    active, err = relationTargetActiveInTx(tx, config)
    if err != nil { _ = tx.Rollback(); t.Fatal(err) }
    if !active { _ = tx.Rollback(); t.Fatal("rolled-back target tombstone remained visible") }
    if err := tx.Rollback(); err != nil { t.Fatal(err) }
}

func TestRelationPropertyUpdateRejectsDeletedTargetAndAmbiguousLegacyConfig(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    source, record := dbRegressionCreate(t, app)
    target, _ := dbRegressionCreate(t, app)
    alternate, _ := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    prop := computedProperty(t, app, source.ID, "Related", "relation", map[string]any{"targetDatabaseId": target.ID})
    original := prop.Config
    update := func(config, name string, want int) {
        t.Helper()
        candidate := prop
        candidate.Name = name
        candidate.Config = config
        payload, err := json.Marshal(candidate)
        if err != nil { t.Fatal(err) }
        result := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(payload), app.handleDBProperties)
        if result.Code != want {
            t.Fatalf("property update: want %d, got %d: %s", want, result.Code, result.Body.String())
        }
        if want == http.StatusOK {
            if err := json.Unmarshal(result.Body.Bytes(), &prop); err != nil { t.Fatal(err) }
        }
    }
    assertUnchanged := func(expectedConfig string) {
        t.Helper()
        var name, config string
        var version int
        if err := app.DB.QueryRow("SELECT name,config,version FROM db_properties WHERE id=?", prop.ID).
            Scan(&name, &config, &version); err != nil { t.Fatal(err) }
        if name != prop.Name || config != expectedConfig || version != prop.Version {
            t.Fatalf("rejected update changed property: name=%q config=%q version=%d", name, config, version)
        }
        var recordVersion int
        if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", record.ID).Scan(&recordVersion); err != nil { t.Fatal(err) }
        if recordVersion != record.Version { t.Fatalf("rejected update changed record version: %d", recordVersion) }
        assertViewIntegrityProviders(t, providerPath, providerBefore)
    }

    if _, err := app.DB.Exec("UPDATE databases SET deleted_at='2026-01-01' WHERE id=?", target.ID); err != nil { t.Fatal(err) }
    update(original, "Must not rename with trashed target", http.StatusBadRequest)
    assertUnchanged(original)
    if _, err := app.DB.Exec("UPDATE databases SET deleted_at=NULL WHERE id=?", target.ID); err != nil { t.Fatal(err) }
    update(original, "Allowed after restore", http.StatusOK)

    // Simulate a legacy row with two conflicting target IDs. Last-key-wins
    // decoding must not allow retargeting it even when all values are empty.
    ambiguous := fmt.Sprintf(`{"targetDatabaseId":%q,"targetDatabaseId":%q}`, target.ID, alternate.ID)
    if _, err := app.DB.Exec("UPDATE db_properties SET config=? WHERE id=?", ambiguous, prop.ID); err != nil { t.Fatal(err) }
    update(fmt.Sprintf(`{"targetDatabaseId":%q}`, alternate.ID), "Must not reinterpret ambiguous target", http.StatusConflict)
    assertUnchanged(ambiguous)

    if _, err := app.DB.Exec("UPDATE db_properties SET config=? WHERE id=?", original, prop.ID); err != nil { t.Fatal(err) }
    update(fmt.Sprintf(`{"targetDatabaseId":%q}`, alternate.ID), "Safe empty retarget", http.StatusOK)
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
