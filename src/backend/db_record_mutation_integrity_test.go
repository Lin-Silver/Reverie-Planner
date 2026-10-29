package main

import (
    "fmt"
    "net/http"
    "testing"
)

// A missing record is a 404; an unavailable SQLite dependency must not be
// reported as a missing record. All fault injection is confined to test data.
func TestDBRecordUpdateDistinguishesMissingFromSQLiteReadFailures(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    _, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    payload := fmt.Sprintf(`{"id":%q,"title":"Updated after repair","version":%d}`, record.ID, record.Version)
    missing := dbRegressionCall(t, app, http.MethodPut, "/api/db/records",
        `{"id":"missing-record","title":"Not saved","version":1}`, app.handleDBRecords)
    if missing.Code != http.StatusNotFound {
        t.Fatalf("missing record should be 404: %d %s", missing.Code, missing.Body.String())
    }

    faults := []struct {
        name, table, oldColumn, newColumn string
    }{
        {"database ownership lookup", "databases", "deleted_at", "deleted_at_unavailable"},
        {"record title lookup in transaction", "db_records", "title", "title_unavailable"},
    }
    for _, fault := range faults {
        t.Run(fault.name, func(t *testing.T) {
            rename := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", fault.table, fault.oldColumn, fault.newColumn)
            if _, err := app.DB.Exec(rename); err != nil { t.Fatal(err) }
            failed := dbRegressionCall(t, app, http.MethodPut, "/api/db/records", payload, app.handleDBRecords)
            if failed.Code != http.StatusInternalServerError {
                t.Fatalf("SQLite lookup failure must return 500, got %d: %s", failed.Code, failed.Body.String())
            }
            restore := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", fault.table, fault.newColumn, fault.oldColumn)
            if _, err := app.DB.Exec(restore); err != nil { t.Fatal(err) }

            saved, err := app.getDBRecord(record.ID)
            if err != nil || saved.Title != record.Title || saved.Version != record.Version {
                t.Fatalf("failed update changed record title/version: %+v %v", saved, err)
            }
            var pageTitle string
            var pageVersion int
            if err := app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?", record.PageID).
                Scan(&pageTitle, &pageVersion); err != nil { t.Fatal(err) }
            if pageTitle != record.Title || pageVersion != 1 {
                t.Fatalf("failed update changed backing page: title=%q version=%d", pageTitle, pageVersion)
            }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
        })
    }

    updated := dbRegressionCall(t, app, http.MethodPut, "/api/db/records", payload, app.handleDBRecords)
    if updated.Code != http.StatusOK {
        t.Fatalf("valid update after fault removal: %d %s", updated.Code, updated.Body.String())
    }
    saved, err := app.getDBRecord(record.ID)
    if err != nil || saved.Title != "Updated after repair" || saved.Version != record.Version+1 {
        t.Fatalf("valid update was not persisted: %+v %v", saved, err)
    }
    var pageTitle string
    var pageVersion int
    if err := app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?", record.PageID).
        Scan(&pageTitle, &pageVersion); err != nil { t.Fatal(err) }
    if pageTitle != "Updated after repair" || pageVersion != 2 {
        t.Fatalf("valid update did not synchronize page title/version: %q %d", pageTitle, pageVersion)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBRecordDeleteRequiresIDWithoutChangingData(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    _, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)
    rejected := dbRegressionCall(t, app, http.MethodDelete, "/api/db/records", "", app.handleDBRecords)
    if rejected.Code != http.StatusBadRequest {
        t.Fatalf("empty delete ID should return 400: %d %s", rejected.Code, rejected.Body.String())
    }
    saved, err := app.getDBRecord(record.ID)
    if err != nil || saved.Version != record.Version || saved.Title != record.Title {
        t.Fatalf("rejected deletion changed record: %+v %v", saved, err)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
