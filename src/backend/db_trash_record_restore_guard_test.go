package main

import (
    "encoding/json"
    "net/http"
    "testing"
)

func TestTrashRecordRestoreRejectsStaleActiveRelationsAndPreservesData(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    source, from := dbRegressionCreate(t, app)
    target, to := dbRegressionCreate(t, app)
    _, other := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    create, err := json.Marshal(DBProperty{
        DatabaseID: source.ID, Name: "Related", Type: "relation",
        Config: `{"targetDatabaseId":"` + target.ID + `"}`,
    })
    if err != nil { t.Fatal(err) }
    created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties",
        string(create), app.handleDBProperties)
    if created.Code != http.StatusCreated {
        t.Fatalf("create relation: %d %s", created.Code, created.Body.String())
    }
    var property DBProperty
    if err := json.Unmarshal(created.Body.Bytes(), &property); err != nil { t.Fatal(err) }

    save, err := json.Marshal(map[string]any{
        "id": from.ID, "version": from.Version,
        "values": map[string]any{property.ID: []string{to.ID}},
    })
    if err != nil { t.Fatal(err) }
    saved := dbRegressionCall(t, app, http.MethodPut, "/api/db/records",
        string(save), app.handleDBRecords)
    if saved.Code != http.StatusOK {
        t.Fatalf("save relation: %d %s", saved.Code, saved.Body.String())
    }
    for _, id := range []string{from.ID, to.ID} {
        deleted := dbRegressionCall(t, app, http.MethodDelete,
            "/api/db/records?id="+id, "", app.handleDBRecords)
        if deleted.Code != http.StatusNoContent {
            t.Fatalf("trash record %s: %d %s", id, deleted.Code, deleted.Body.String())
        }
    }

    var version, pageVersion int
    var deletedAt, raw string
    if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?",
        from.ID).Scan(&version, &deletedAt); err != nil { t.Fatal(err) }
    if err := app.DB.QueryRow("SELECT version FROM pages WHERE id=?",
        from.PageID).Scan(&pageVersion); err != nil { t.Fatal(err) }
    if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
        from.ID, property.ID).Scan(&raw); err != nil { t.Fatal(err) }
    original := raw
    payload := `{"kind":"record","id":"` + from.ID + `"}`
    attempt := func(want int) {
        t.Helper()
        response := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
            payload, app.handleDBTrash)
        if response.Code != want {
            t.Fatalf("restore record: expected %d, got %d %s",
                want, response.Code, response.Body.String())
        }
    }
    assertUnchanged := func(wantValue string) {
        t.Helper()
        var currentVersion, currentPageVersion int
        var currentDeleted, currentValue string
        if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?",
            from.ID).Scan(&currentVersion, &currentDeleted); err != nil { t.Fatal(err) }
        if currentVersion != version || currentDeleted != deletedAt {
            t.Fatalf("failed restore changed tombstone: (%d,%q) -> (%d,%q)",
                version, deletedAt, currentVersion, currentDeleted)
        }
        if err := app.DB.QueryRow("SELECT version FROM pages WHERE id=?",
            from.PageID).Scan(&currentPageVersion); err != nil { t.Fatal(err) }
        if currentPageVersion != pageVersion {
            t.Fatalf("failed restore changed backing page version: %d -> %d", pageVersion, currentPageVersion)
        }
        if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
            from.ID, property.ID).Scan(&currentValue); err != nil { t.Fatal(err) }
        if currentValue != wantValue {
            t.Fatalf("failed restore changed relation value: %s != %s", currentValue, wantValue)
        }
        assertViewIntegrityProviders(t, providerPath, providerBefore)
    }
    attempt(http.StatusConflict)
    assertUnchanged(original)

    for _, invalid := range []string{
        `["` + other.ID + `"]`, // wrong target database
        `{"invalid":true}`,      // malformed legacy value
        `["` + to.ID + `","` + to.ID + `"]`, // duplicate IDs
    } {
        if _, err := app.DB.Exec("UPDATE db_values SET value=? WHERE record_id=? AND property_id=?",
            invalid, from.ID, property.ID); err != nil { t.Fatal(err) }
        attempt(http.StatusConflict)
        assertUnchanged(invalid)
    }
    if _, err := app.DB.Exec("UPDATE db_values SET value=? WHERE record_id=? AND property_id=?",
        original, from.ID, property.ID); err != nil { t.Fatal(err) }

    if _, err := app.DB.Exec("ALTER TABLE db_values RENAME COLUMN value TO unavailable_value"); err != nil {
        t.Fatal(err)
    }
    attempt(http.StatusInternalServerError)
    if _, err := app.DB.Exec("ALTER TABLE db_values RENAME COLUMN unavailable_value TO value"); err != nil {
        t.Fatal(err)
    }
    assertUnchanged(original)

    restoreTarget := `{"kind":"record","id":"` + to.ID + `"}`
    restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
        restoreTarget, app.handleDBTrash)
    if restored.Code != http.StatusOK {
        t.Fatalf("restore target: %d %s", restored.Code, restored.Body.String())
    }
    trashedDB := dbRegressionCall(t, app, http.MethodDelete,
        "/api/databases?id="+target.ID, "", app.handleDatabases)
    if trashedDB.Code != http.StatusNoContent {
        t.Fatalf("trash target database: %d %s", trashedDB.Code, trashedDB.Body.String())
    }
    attempt(http.StatusConflict)
    assertUnchanged(original)
    restoredDB := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
        `{"kind":"database","id":"`+target.ID+`"}`, app.handleDBTrash)
    if restoredDB.Code != http.StatusOK {
        t.Fatalf("restore target database: %d %s", restoredDB.Code, restoredDB.Body.String())
    }

    attempt(http.StatusOK)
    var finalVersion int
    if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?",
        from.ID).Scan(&finalVersion); err != nil { t.Fatal(err) }
    if finalVersion != version+1 {
        t.Fatalf("successful restore should increment version once: %d -> %d", version, finalVersion)
    }
    record, err := app.getDBRecord(from.ID)
    if err != nil { t.Fatal(err) }
    ids, ok := record.Values[property.ID].([]any)
    if !ok || len(ids) != 1 || ids[0] != to.ID {
        t.Fatalf("restored record lost relation: %#v", record.Values[property.ID])
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestTrashRecordRestoreDefersDeletedRelationPropertyValidation(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    source, from := dbRegressionCreate(t, app)
    target, to := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)
    created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties",
        `{"databaseId":"`+source.ID+`","name":"Related","type":"relation","config":"{\\"targetDatabaseId\\":\\"`+target.ID+`\\"}"}`,
        app.handleDBProperties)
    if created.Code != http.StatusCreated {
        t.Fatalf("create relation: %d %s", created.Code, created.Body.String())
    }
    var property DBProperty
    if err := json.Unmarshal(created.Body.Bytes(), &property); err != nil { t.Fatal(err) }
    body, err := json.Marshal(map[string]any{
        "id": from.ID, "version": from.Version,
        "values": map[string]any{property.ID: []string{to.ID}},
    })
    if err != nil { t.Fatal(err) }
    saved := dbRegressionCall(t, app, http.MethodPut, "/api/db/records",
        string(body), app.handleDBRecords)
    if saved.Code != http.StatusOK {
        t.Fatalf("save relation: %d %s", saved.Code, saved.Body.String())
    }
    for _, tc := range []struct{path string; handler http.HandlerFunc}{
        {"/api/db/records?id="+from.ID, app.handleDBRecords},
        {"/api/db/properties?id="+property.ID, app.handleDBProperties},
        {"/api/db/records?id="+to.ID, app.handleDBRecords},
    } {
        result := dbRegressionCall(t, app, http.MethodDelete, tc.path, "", tc.handler)
        if result.Code != http.StatusNoContent {
            t.Fatalf("delete %s: %d %s", tc.path, result.Code, result.Body.String())
        }
    }
    restore := func(kind, id string, want int) {
        t.Helper()
        response := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
            `{"kind":"`+kind+`","id":"`+id+`"}`, app.handleDBTrash)
        if response.Code != want {
            t.Fatalf("restore %s: expected %d got %d %s", kind, want, response.Code, response.Body.String())
        }
    }
    restore("record", from.ID, http.StatusOK) // deleted property remains invisible
    restore("property", property.ID, http.StatusConflict)
    restore("record", to.ID, http.StatusOK)
    restore("property", property.ID, http.StatusOK)
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
