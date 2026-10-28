package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

func TestDBViewDeleteRejectsStaleVersionWithoutLosingRemoteChanges(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    _, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    create := dbRegressionCall(t, app, http.MethodPost, "/api/db/views",
        fmt.Sprintf(`{"databaseId":%q,"name":"Second view","type":"table","config":"{}"}`, record.DatabaseID), app.handleDBViews)
    if create.Code != http.StatusCreated {
        t.Fatalf("create second view: %d %s", create.Code, create.Body.String())
    }
    var original DBView
    if err := json.Unmarshal(create.Body.Bytes(), &original); err != nil { t.Fatal(err) }

    rename := dbRegressionCall(t, app, http.MethodPut, "/api/db/views",
        fmt.Sprintf(`{"id":%q,"version":%d,"name":"Remote rename","type":"table","config":"{}"}`,
            original.ID, original.Version), app.handleDBViews)
    if rename.Code != http.StatusOK {
        t.Fatalf("rename view: %d %s", rename.Code, rename.Body.String())
    }
    var current DBView
    if err := json.Unmarshal(rename.Body.Bytes(), &current); err != nil { t.Fatal(err) }
    if current.Version != original.Version+1 { t.Fatalf("expected incremented version, got %d", current.Version) }

    path := "/api/db/views?id=" + original.ID
    for _, tc := range []struct {
        name, suffix string
        want int
    }{
        {"malformed version", "&version=invalid", http.StatusBadRequest},
        {"zero version", "&version=0", http.StatusBadRequest},
        {"stale version", fmt.Sprintf("&version=%d", original.Version), http.StatusConflict},
    } {
        t.Run(tc.name, func(t *testing.T) {
            failed := dbRegressionCall(t, app, http.MethodDelete, path+tc.suffix, "", app.handleDBViews)
            if failed.Code != tc.want {
                t.Fatalf("delete: got %d, want %d: %s", failed.Code, tc.want, failed.Body.String())
            }
            var name string
            var version int
            var tombstone sql.NullString
            if err := app.DB.QueryRow("SELECT name,version,deleted_at FROM db_views WHERE id=?", original.ID).
                Scan(&name, &version, &tombstone); err != nil { t.Fatal(err) }
            if name != current.Name || version != current.Version || tombstone.Valid {
                t.Fatalf("rejected deletion changed remote view: name=%q version=%d deleted=%v", name, version, tombstone)
            }
            if _, err := app.getDBRecord(record.ID); err != nil { t.Fatalf("original record lost: %v", err) }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
        })
    }

    deleted := dbRegressionCall(t, app, http.MethodDelete,
        path+fmt.Sprintf("&version=%d", current.Version), "", app.handleDBViews)
    if deleted.Code != http.StatusNoContent {
        t.Fatalf("delete with latest version: %d %s", deleted.Code, deleted.Body.String())
    }
    var version int
    var tombstone sql.NullString
    if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_views WHERE id=?", original.ID).
        Scan(&version, &tombstone); err != nil { t.Fatal(err) }
    if !tombstone.Valid || version != current.Version+1 {
        t.Fatalf("latest version deletion must tombstone exactly once: version=%d deleted=%v", version, tombstone)
    }
    if _, err := app.getDBRecord(record.ID); err != nil { t.Fatalf("original record lost: %v", err) }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBViewDeleteWithoutVersionRemainsCompatible(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    _, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)
    create := dbRegressionCall(t, app, http.MethodPost, "/api/db/views",
        fmt.Sprintf(`{"databaseId":%q,"name":"Legacy view","type":"table","config":"{}"}`, record.DatabaseID), app.handleDBViews)
    if create.Code != http.StatusCreated {
        t.Fatalf("create view: %d %s", create.Code, create.Body.String())
    }
    var view DBView
    if err := json.Unmarshal(create.Body.Bytes(), &view); err != nil { t.Fatal(err) }
    deleted := dbRegressionCall(t, app, http.MethodDelete, "/api/db/views?id="+view.ID, "", app.handleDBViews)
    if deleted.Code != http.StatusNoContent {
        t.Fatalf("legacy versionless delete: %d %s", deleted.Code, deleted.Body.String())
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
