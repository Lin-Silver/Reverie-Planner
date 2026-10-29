package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

// Record writes must not modify the data that a trashed database will later
// restore. The parent check is performed in the same SQLite transaction as
// the record, property-value and backing-page writes.
func TestRecordPutRejectsTrashedParentAndPreservesRestoreData(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()

    database, record := dbRegressionCreate(t, app)
    property := computedProperty(t, app, database.ID, "Notes", "text", map[string]any{})
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    deleted := dbRegressionCall(t, app, http.MethodDelete,
        fmt.Sprintf("/api/databases?id=%s&version=%d", database.ID, database.Version),
        "", app.handleDatabases)
    if deleted.Code != http.StatusNoContent {
        t.Fatalf("trash database: %d %s", deleted.Code, deleted.Body.String())
    }

    for _, tc := range []struct {
        name, title string
        values map[string]any
    }{
        {"title only", "Do not overwrite page", map[string]any{}},
        {"property only", "", map[string]any{property.ID: "Do not persist"}},
        {"title and property", "Do not overwrite page", map[string]any{property.ID: "Do not persist"}},
    } {
        t.Run(tc.name, func(t *testing.T) {
            body, err := json.Marshal(map[string]any{
                "id": record.ID, "version": record.Version, "title": tc.title, "values": tc.values,
            })
            if err != nil { t.Fatal(err) }
            response := dbRegressionCall(t, app, http.MethodPut,
                "/api/db/records", string(body), app.handleDBRecords)
            if response.Code != http.StatusNotFound {
                t.Fatalf("write to trashed database: got %d, want 404: %s", response.Code, response.Body.String())
            }
            var recordTitle, pageTitle string
            var recordVersion, pageVersion, valueCount int
            var recordDeleted sql.NullString
            if err := app.DB.QueryRow("SELECT title,version,deleted_at FROM db_records WHERE id=?", record.ID).
                Scan(&recordTitle, &recordVersion, &recordDeleted); err != nil { t.Fatal(err) }
            if err := app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?", record.PageID).
                Scan(&pageTitle, &pageVersion); err != nil { t.Fatal(err) }
            if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_values WHERE record_id=? AND property_id=?",
                record.ID, property.ID).Scan(&valueCount); err != nil { t.Fatal(err) }
            if recordTitle != record.Title || recordVersion != record.Version || recordDeleted.Valid ||
                pageTitle != record.Title || pageVersion != 1 || valueCount != 0 {
                t.Fatalf("rejected update changed restorable data: record=%q v%d deleted=%v page=%q v%d values=%d",
                    recordTitle, recordVersion, recordDeleted, pageTitle, pageVersion, valueCount)
            }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
        })
    }

    restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
        fmt.Sprintf(`{"kind":"database","id":%q}`, database.ID), app.handleDBTrash)
    if restored.Code != http.StatusOK {
        t.Fatalf("restore database: %d %s", restored.Code, restored.Body.String())
    }
    body, err := json.Marshal(map[string]any{
        "id": record.ID, "version": record.Version, "title": "Updated after restore",
        "values": map[string]any{property.ID: "Preserved note"},
    })
    if err != nil { t.Fatal(err) }
    updated := dbRegressionCall(t, app, http.MethodPut,
        "/api/db/records", string(body), app.handleDBRecords)
    if updated.Code != http.StatusOK {
        t.Fatalf("write after restore: %d %s", updated.Code, updated.Body.String())
    }
    var actual DBRecord
    if err := json.Unmarshal(updated.Body.Bytes(), &actual); err != nil { t.Fatal(err) }
    if actual.Title != "Updated after restore" || actual.Version != record.Version+1 ||
        actual.Values[property.ID] != "Preserved note" {
        t.Fatalf("write after restore did not persist expected values: %+v", actual)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
