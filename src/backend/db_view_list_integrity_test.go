package main

import (
    "encoding/json"
    "net/http"
    "testing"
)

// A missing or soft-deleted database must not masquerade as an existing
// database with zero views. Read faults must not become successful empty lists.
func TestDBViewListChecksOwnerAndPreservesStateOnFailures(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)
    originalView := database.Views[0]

    check := func(method, target string, want int) {
        t.Helper()
        response := dbRegressionCall(t, app, method, target, "", app.handleDBViews)
        if response.Code != want {
            t.Fatalf("%s %s: want %d, got %d: %s", method, target, want, response.Code, response.Body.String())
        }
    }
    check(http.MethodGet, "/api/db/views", http.StatusBadRequest)
    check(http.MethodGet, "/api/db/views?databaseId=nonexistent", http.StatusNotFound)
    check(http.MethodDelete, "/api/db/views", http.StatusBadRequest)

    good := dbRegressionCall(t, app, http.MethodGet, "/api/db/views?databaseId="+database.ID, "", app.handleDBViews)
    if good.Code != http.StatusOK {
        t.Fatalf("list existing views: %d %s", good.Code, good.Body.String())
    }
    var views []DBView
    if err := json.Unmarshal(good.Body.Bytes(), &views); err != nil {
        t.Fatal(err)
    }
    if len(views) != 1 || views[0].ID != originalView.ID {
        t.Fatalf("existing database views unexpectedly missing: %+v", views)
    }

    if _, err := app.DB.Exec("ALTER TABLE databases RENAME COLUMN deleted_at TO unavailable_deleted_at"); err != nil {
        t.Fatal(err)
    }
    check(http.MethodGet, "/api/db/views?databaseId="+database.ID, http.StatusInternalServerError)
    if _, err := app.DB.Exec("ALTER TABLE databases RENAME COLUMN unavailable_deleted_at TO deleted_at"); err != nil {
        t.Fatal(err)
    }

    if _, err := app.DB.Exec("ALTER TABLE db_views RENAME TO db_views_unavailable"); err != nil {
        t.Fatal(err)
    }
    check(http.MethodGet, "/api/db/views?databaseId="+database.ID, http.StatusInternalServerError)
    if _, err := app.DB.Exec("ALTER TABLE db_views_unavailable RENAME TO db_views"); err != nil {
        t.Fatal(err)
    }

    var version int
    var deletedAt *string
    if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_views WHERE id=?", originalView.ID).Scan(&version, &deletedAt); err != nil {
        t.Fatal(err)
    }
    if version != originalView.Version || deletedAt != nil {
        t.Fatalf("rejected view requests changed view state: version=%d deleted=%v", version, deletedAt)
    }
    saved, err := app.getDBRecord(record.ID)
    if err != nil || saved.Title != record.Title || saved.Version != record.Version {
        t.Fatalf("view read failures changed record state: %+v %v", saved, err)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)

    deleted := dbRegressionCall(t, app, http.MethodDelete, "/api/databases?id="+database.ID, "", app.handleDatabases)
    if deleted.Code != http.StatusNoContent {
        t.Fatalf("soft-delete database: %d %s", deleted.Code, deleted.Body.String())
    }
    check(http.MethodGet, "/api/db/views?databaseId="+database.ID, http.StatusNotFound)
    if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_views WHERE id=?", originalView.ID).Scan(&version, &deletedAt); err != nil {
        t.Fatal(err)
    }
    if version != originalView.Version || deletedAt != nil {
        t.Fatalf("database deletion unexpectedly changed view state: version=%d deleted=%v", version, deletedAt)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
