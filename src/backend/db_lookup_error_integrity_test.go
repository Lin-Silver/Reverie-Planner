package main

import (
    "encoding/json"
    "net/http"
    "testing"
)

// A database that exists but has an unreadable property table must not be
// reported as absent. Faults are confined to the disposable test SQLite DB.
func TestDatabaseGetDistinguishesMissingFromNestedSQLiteFailure(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    missing := dbRegressionCall(t, app, http.MethodGet,
        "/api/databases?id=missing-database", "", app.handleDatabases)
    if missing.Code != http.StatusNotFound {
        t.Fatalf("missing database should be 404: %d %s", missing.Code, missing.Body.String())
    }

    if _, err := app.DB.Exec("ALTER TABLE db_properties RENAME TO db_properties_unavailable"); err != nil {
        t.Fatal(err)
    }
    failed := dbRegressionCall(t, app, http.MethodGet,
        "/api/databases?id="+database.ID, "", app.handleDatabases)
    if failed.Code != http.StatusInternalServerError {
        t.Fatalf("nested SQLite read failure must return 500, got %d: %s", failed.Code, failed.Body.String())
    }
    if _, err := app.DB.Exec("ALTER TABLE db_properties_unavailable RENAME TO db_properties"); err != nil {
        t.Fatal(err)
    }

    saved, err := app.getDBRecord(record.ID)
    if err != nil || saved.Title != record.Title || saved.Version != record.Version {
        t.Fatalf("failed database read changed record: %+v %v", saved, err)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)

    recovered := dbRegressionCall(t, app, http.MethodGet,
        "/api/databases?id="+database.ID, "", app.handleDatabases)
    if recovered.Code != http.StatusOK {
        t.Fatalf("database read after repair: %d %s", recovered.Code, recovered.Body.String())
    }
    var actual Database
    if err := json.Unmarshal(recovered.Body.Bytes(), &actual); err != nil { t.Fatal(err) }
    if actual.ID != database.ID || len(actual.Properties) != len(database.Properties) ||
        len(actual.Views) != len(database.Views) {
        t.Fatalf("database metadata changed after read fault: %+v", actual)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}

// Property update performs an ownership lookup before writing. An SQL fault
// in that lookup must be a server error, not a false missing-property response.
func TestDBPropertyUpdateDistinguishesMissingFromSQLiteFailure(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)
    if len(database.Properties) == 0 { t.Fatal("database has no title property") }
    title := database.Properties[0]
    title.Name = "Name after repair"
    payload, err := json.Marshal(title)
    if err != nil { t.Fatal(err) }

    missing := title
    missing.ID = "missing-property"
    missingJSON, err := json.Marshal(missing)
    if err != nil { t.Fatal(err) }
    absent := dbRegressionCall(t, app, http.MethodPut,
        "/api/db/properties", string(missingJSON), app.handleDBProperties)
    if absent.Code != http.StatusNotFound {
        t.Fatalf("missing property should be 404: %d %s", absent.Code, absent.Body.String())
    }

    if _, err := app.DB.Exec("ALTER TABLE db_properties RENAME TO db_properties_unavailable"); err != nil {
        t.Fatal(err)
    }
    failed := dbRegressionCall(t, app, http.MethodPut,
        "/api/db/properties", string(payload), app.handleDBProperties)
    if failed.Code != http.StatusInternalServerError {
        t.Fatalf("property ownership SQLite fault must be 500, got %d: %s", failed.Code, failed.Body.String())
    }
    if _, err := app.DB.Exec("ALTER TABLE db_properties_unavailable RENAME TO db_properties"); err != nil {
        t.Fatal(err)
    }

    var savedName string
    var savedVersion int
    if err := app.DB.QueryRow("SELECT name,version FROM db_properties WHERE id=?", title.ID).
        Scan(&savedName, &savedVersion); err != nil { t.Fatal(err) }
    if savedName != database.Properties[0].Name || savedVersion != database.Properties[0].Version {
        t.Fatalf("rejected update changed property: name=%q version=%d", savedName, savedVersion)
    }
    savedRecord, err := app.getDBRecord(record.ID)
    if err != nil || savedRecord.Title != record.Title || savedRecord.Version != record.Version {
        t.Fatalf("failed property update changed record: %+v %v", savedRecord, err)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)

    recovered := dbRegressionCall(t, app, http.MethodPut,
        "/api/db/properties", string(payload), app.handleDBProperties)
    if recovered.Code != http.StatusOK {
        t.Fatalf("valid property update after repair: %d %s", recovered.Code, recovered.Body.String())
    }
    if err := app.DB.QueryRow("SELECT name,version FROM db_properties WHERE id=?", title.ID).
        Scan(&savedName, &savedVersion); err != nil { t.Fatal(err) }
    if savedName != title.Name || savedVersion != title.Version+1 {
        t.Fatalf("valid update not persisted: name=%q version=%d", savedName, savedVersion)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
