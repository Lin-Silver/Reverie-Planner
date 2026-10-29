package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "testing"
)

// Existing pages can host a database through the page editor, but no page
// should become the backing page of two databases or of a record and database.
func TestDatabaseCreateRejectsConflictingBackingPages(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()

    liveDB, liveRecord := dbRegressionCreate(t, app)
    trashedDB, _ := dbRegressionCreate(t, app)
    _, trashedRecord := dbRegressionCreate(t, app)

    removed := dbRegressionCall(t, app, http.MethodDelete,
        "/api/databases?id="+trashedDB.ID, "", app.handleDatabases)
    if removed.Code != http.StatusNoContent {
        t.Fatalf("trash database: %d %s", removed.Code, removed.Body.String())
    }
    removed = dbRegressionCall(t, app, http.MethodDelete,
        "/api/db/records?id="+trashedRecord.ID, "", app.handleDBRecords)
    if removed.Code != http.StatusNoContent {
        t.Fatalf("trash record: %d %s", removed.Code, removed.Body.String())
    }

    // This is the legitimate page-editor flow: a pre-existing free page is
    // supplied as pageId, instead of allocating a new backing page.
    created := dbRegressionCall(t, app, http.MethodPost, "/api/pages",
        `{"title":"Existing host","workspace":true,"content":"Keep this draft"}`, app.handlePages)
    if created.Code != http.StatusCreated {
        t.Fatalf("create host page: %d %s", created.Code, created.Body.String())
    }
    var host Page
    if err := json.Unmarshal(created.Body.Bytes(), &host); err != nil { t.Fatal(err) }

    providerPath := filepath.Join(app.RootPath, "config", "providers.json")
    providerBefore, err := os.ReadFile(providerPath)
    if err != nil { t.Fatal(err) }

    counts := func() [4]int {
        t.Helper()
        var got [4]int
        for i, table := range []string{"pages", "databases", "db_properties", "db_views"} {
            if err := app.DB.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&got[i]); err != nil {
                t.Fatal(err)
            }
        }
        return got
    }
    checkHost := func() {
        t.Helper()
        var title, content string
        var version int
        if err := app.DB.QueryRow("SELECT title, content, version FROM pages WHERE id=?", host.ID).
            Scan(&title, &content, &version); err != nil { t.Fatal(err) }
        if title != host.Title || content != host.Content || version != host.Version {
            t.Fatalf("host page changed: title=%q content=%q version=%d; want %+v",
                title, content, version, host)
        }
        providerAfter, err := os.ReadFile(providerPath)
        if err != nil || !bytes.Equal(providerBefore, providerAfter) {
            t.Fatalf("provider config changed: %v", err)
        }
    }

    // A missing page and both live and soft-deleted ownership must be rejected
    // before any new database, default property or view is persisted.
    for _, tc := range []struct{ name, pageID string }{
        {"missing page", "page-does-not-exist"},
        {"live database page", liveDB.PageID},
        {"live record page", liveRecord.PageID},
        {"trashed database page", trashedDB.PageID},
        {"trashed record page", trashedRecord.PageID},
    } {
        t.Run(tc.name, func(t *testing.T) {
            before := counts()
            payload, err := json.Marshal(map[string]string{
                "title": "Should not exist", "pageId": tc.pageID,
            })
            if err != nil { t.Fatal(err) }
            response := dbRegressionCall(t, app, http.MethodPost,
                "/api/databases", string(payload), app.handleDatabases)
            if response.Code != http.StatusConflict {
                t.Fatalf("expected 409, got %d: %s", response.Code, response.Body.String())
            }
            if after := counts(); after != before {
                t.Fatalf("invalid database creation mutated rows: before=%v after=%v", before, after)
            }
            checkHost()
        })
    }

    before := counts()
    payload := fmt.Sprintf(`{"title":"Existing host","pageId":%q}`, host.ID)
    response := dbRegressionCall(t, app, http.MethodPost,
        "/api/databases", payload, app.handleDatabases)
    if response.Code != http.StatusCreated {
        t.Fatalf("legitimate existing-page database creation failed: %d %s",
            response.Code, response.Body.String())
    }
    var database Database
    if err := json.Unmarshal(response.Body.Bytes(), &database); err != nil { t.Fatal(err) }
    if database.PageID != host.ID {
        t.Fatalf("created database lost host page: got=%q want=%q", database.PageID, host.ID)
    }
    after := counts()
    if after != [4]int{before[0], before[1]+1, before[2]+1, before[3]+1} {
        t.Fatalf("unexpected database/default schema creation: before=%v after=%v", before, after)
    }
    checkHost()

    // The successful binding must also make a second binding fail atomically.
    duplicate := dbRegressionCall(t, app, http.MethodPost,
        "/api/databases", payload, app.handleDatabases)
    if duplicate.Code != http.StatusConflict {
        t.Fatalf("duplicate host page accepted: %d %s", duplicate.Code, duplicate.Body.String())
    }
    if got := counts(); got != after {
        t.Fatalf("duplicate host page changed row counts: before=%v after=%v", after, got)
    }
    checkHost()
}
