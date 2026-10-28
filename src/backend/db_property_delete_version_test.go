package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

// A stale browser must not tombstone a property renamed by another browser.
func TestDBPropertyDeleteRejectsStaleVersionAndPreservesRemoteEdit(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    original := computedProperty(t, app, database.ID, "Notes", "text", map[string]any{})
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    rename := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties",
        fmt.Sprintf(`{"id":%q,"databaseId":%q,"name":"Remote notes","type":"text","config":"{}","visible":true,"version":%d}`,
            original.ID, original.DatabaseID, original.Version), app.handleDBProperties)
    if rename.Code != http.StatusOK {
        t.Fatalf("rename property: %d %s", rename.Code, rename.Body.String())
    }
    var current DBProperty
    if err := json.Unmarshal(rename.Body.Bytes(), &current); err != nil { t.Fatal(err) }
    if current.Version != original.Version+1 || current.Name != "Remote notes" {
        t.Fatalf("remote rename was not persisted: %+v", current)
    }

    path := "/api/db/properties?id=" + original.ID
    for _, tc := range []struct {
        name, suffix string
        want int
    }{
        {"malformed version", "&version=invalid", http.StatusBadRequest},
        {"zero version", "&version=0", http.StatusBadRequest},
        {"negative version", "&version=-1", http.StatusBadRequest},
        {"stale version", fmt.Sprintf("&version=%d", original.Version), http.StatusConflict},
    } {
        t.Run(tc.name, func(t *testing.T) {
            response := dbRegressionCall(t, app, http.MethodDelete, path+tc.suffix, "", app.handleDBProperties)
            if response.Code != tc.want {
                t.Fatalf("delete: got %d want %d: %s", response.Code, tc.want, response.Body.String())
            }
            var name string
            var version int
            var deleted sql.NullString
            if err := app.DB.QueryRow("SELECT name,version,deleted_at FROM db_properties WHERE id=?", original.ID).
                Scan(&name, &version, &deleted); err != nil { t.Fatal(err) }
            if name != current.Name || version != current.Version || deleted.Valid {
                t.Fatalf("rejected deletion changed remote property: name=%q version=%d deleted=%v", name, version, deleted)
            }
            if _, err := app.getDBRecord(record.ID); err != nil { t.Fatalf("original record lost: %v", err) }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
        })
    }

    deleted := dbRegressionCall(t, app, http.MethodDelete,
        path+fmt.Sprintf("&version=%d", current.Version), "", app.handleDBProperties)
    if deleted.Code != http.StatusNoContent {
        t.Fatalf("delete latest version: %d %s", deleted.Code, deleted.Body.String())
    }
    var version int
    var tombstone sql.NullString
    if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_properties WHERE id=?", original.ID).
        Scan(&version, &tombstone); err != nil { t.Fatal(err) }
    if !tombstone.Valid || version != current.Version+1 {
        t.Fatalf("latest version deletion must tombstone once: version=%d deleted=%v", version, tombstone)
    }
    if _, err := app.getDBRecord(record.ID); err != nil { t.Fatalf("original record lost: %v", err) }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBPropertyDeleteLegacyVersionlessCompatibility(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    property := computedProperty(t, app, database.ID, "Legacy notes", "text", map[string]any{})
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    deleted := dbRegressionCall(t, app, http.MethodDelete,
        "/api/db/properties?id="+property.ID, "", app.handleDBProperties)
    if deleted.Code != http.StatusNoContent {
        t.Fatalf("legacy versionless delete: %d %s", deleted.Code, deleted.Body.String())
    }
    if _, err := app.getDBRecord(record.ID); err != nil { t.Fatalf("original record lost: %v", err) }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
