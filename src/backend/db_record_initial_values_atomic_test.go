package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

func TestDBRecordCreatePersistsTypedInitialValuesAndRelation(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, existing := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    createProperty := func(name, kind, config string) DBProperty {
        t.Helper()
        body := fmt.Sprintf(`{"databaseId":%q,"name":%q,"type":%q,"config":%q}`,
            database.ID, name, kind, config)
        response := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", body, app.handleDBProperties)
        if response.Code != http.StatusCreated {
            t.Fatalf("create %s: %d %s", name, response.Code, response.Body.String())
        }
        var property DBProperty
        if err := json.Unmarshal(response.Body.Bytes(), &property); err != nil {
            t.Fatal(err)
        }
        return property
    }
    textProperty := createProperty("Description", "text", "{}")
    selectProperty := createProperty("Status", "select", `{"options":["Open","Closed"]}`)
    relationProperty := createProperty("Related", "relation",
        fmt.Sprintf(`{"targetDatabaseId":%q}`, database.ID))

    values := map[string]any{
        textProperty.ID: "Saved during create",
        selectProperty.ID: "Open",
        relationProperty.ID: []string{existing.ID},
    }
    payload, err := json.Marshal(map[string]any{
        "id": "rec_atomic_initial_values",
        "databaseId": database.ID,
        "title": "Atomic create",
        "values": values,
    })
    if err != nil { t.Fatal(err) }
    response := dbRegressionCall(t, app, http.MethodPost, "/api/db/records", string(payload), app.handleDBRecords)
    if response.Code != http.StatusCreated {
        t.Fatalf("create with typed values: %d %s", response.Code, response.Body.String())
    }
    var created DBRecord
    if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil { t.Fatal(err) }
    if created.Version != 1 || created.PageID == "" || created.Title != "Atomic create" {
        t.Fatalf("incomplete created record: %+v", created)
    }
    if created.Values[textProperty.ID] != "Saved during create" || created.Values[selectProperty.ID] != "Open" {
        t.Fatalf("initial text/select values absent from response: %+v", created.Values)
    }
    related, ok := created.Values[relationProperty.ID].([]any)
    if !ok || len(related) != 1 || related[0] != existing.ID {
        t.Fatalf("initial relation absent from response: %+v", created.Values)
    }
    var persistedValues int
    if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_values WHERE record_id=?", created.ID).Scan(&persistedValues); err != nil {
        t.Fatal(err)
    }
    if persistedValues != 3 { t.Fatalf("wanted three atomic property values, got %d", persistedValues) }
    var pageTitle string
    if err := app.DB.QueryRow("SELECT title FROM pages WHERE id=?", created.PageID).Scan(&pageTitle); err != nil || pageTitle != created.Title {
        t.Fatalf("backing page not committed with record: %q %v", pageTitle, err)
    }
    loaded, err := app.getDBRecord(created.ID)
    if err != nil || loaded.Values[textProperty.ID] != "Saved during create" || loaded.Values[selectProperty.ID] != "Open" {
        t.Fatalf("initial values not readable after commit: %+v %v", loaded, err)
    }
    original, err := app.getDBRecord(existing.ID)
    if err != nil || original.Version != existing.Version || original.Title != existing.Title {
        t.Fatalf("initial values changed existing record: %+v %v", original, err)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBRecordCreateInitialValuesValidationAndSQLiteFailureRollback(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, original := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    makeProperty := func(kind, config string) DBProperty {
        t.Helper()
        body := fmt.Sprintf(`{"databaseId":%q,"name":%q,"type":%q,"config":%q}`,
            database.ID, "Initial "+kind, kind, config)
        response := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", body, app.handleDBProperties)
        if response.Code != http.StatusCreated { t.Fatalf("create %s: %d %s", kind, response.Code, response.Body.String()) }
        var property DBProperty
        if err := json.Unmarshal(response.Body.Bytes(), &property); err != nil { t.Fatal(err) }
        return property
    }
    textProperty := makeProperty("text", "{}")
    selectProperty := makeProperty("select", `{"options":["Allowed"]}`)
    computedProperty := makeProperty("created_time", "{}")

    var recordsBefore, pagesBefore, valuesBefore int
    for _, check := range []struct { query string; out *int }{
        {"SELECT COUNT(*) FROM db_records", &recordsBefore},
        {"SELECT COUNT(*) FROM pages", &pagesBefore},
        {"SELECT COUNT(*) FROM db_values", &valuesBefore},
    } {
        if err := app.DB.QueryRow(check.query).Scan(check.out); err != nil { t.Fatal(err) }
    }
    scenarios := []struct {
        name, id string
        values map[string]any
        want int
        sqliteFault bool
    }{
        {"invalid select option", "rec_bad_initial_select",
            map[string]any{textProperty.ID: "Should roll back", selectProperty.ID: "Unknown"}, http.StatusBadRequest, false},
        {"read-only timestamp", "rec_bad_initial_computed",
            map[string]any{computedProperty.ID: "2026-10-28"}, http.StatusBadRequest, false},
        {"SQLite value insertion failure", "rec_failed_initial_insert",
            map[string]any{textProperty.ID: "Should roll back"}, http.StatusInternalServerError, true},
    }
    for _, tc := range scenarios {
        t.Run(tc.name, func(t *testing.T) {
            if tc.sqliteFault {
                _, err := app.DB.Exec(`CREATE TRIGGER fail_initial_value BEFORE INSERT ON db_values
                    WHEN NEW.record_id='rec_failed_initial_insert'
                    BEGIN SELECT RAISE(ABORT, 'test value insert failure'); END`)
                if err != nil { t.Fatal(err) }
            }
            payload, err := json.Marshal(map[string]any{
                "id": tc.id, "databaseId": database.ID, "title": "Should not exist", "values": tc.values,
            })
            if err != nil { t.Fatal(err) }
            response := dbRegressionCall(t, app, http.MethodPost, "/api/db/records", string(payload), app.handleDBRecords)
            if response.Code != tc.want {
                t.Fatalf("expected %d, got %d: %s", tc.want, response.Code, response.Body.String())
            }
            var records, pages, values, leaked int
            for _, check := range []struct { query string; args []any; out *int }{
                {"SELECT COUNT(*) FROM db_records", nil, &records},
                {"SELECT COUNT(*) FROM pages", nil, &pages},
                {"SELECT COUNT(*) FROM db_values", nil, &values},
                {"SELECT COUNT(*) FROM db_records WHERE id=?", []any{tc.id}, &leaked},
            } {
                if err := app.DB.QueryRow(check.query, check.args...).Scan(check.out); err != nil { t.Fatal(err) }
            }
            if records != recordsBefore || pages != pagesBefore || values != valuesBefore || leaked != 0 {
                t.Fatalf("failed create persisted rows: records=%d/%d pages=%d/%d values=%d/%d leaked=%d",
                    records, recordsBefore, pages, pagesBefore, values, valuesBefore, leaked)
            }
            var version int
            if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", original.ID).Scan(&version); err != nil || version != original.Version {
                t.Fatalf("original record changed: version=%d err=%v", version, err)
            }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
            if tc.sqliteFault {
                if _, err := app.DB.Exec("DROP TRIGGER fail_initial_value"); err != nil { t.Fatal(err) }
                retry := dbRegressionCall(t, app, http.MethodPost, "/api/db/records", string(payload), app.handleDBRecords)
                if retry.Code != http.StatusCreated { t.Fatalf("retry after removing fault: %d %s", retry.Code, retry.Body.String()) }
                // Keep the other scenarios' baseline stable: the successful retry
                // is checked last and must not be mistaken for a failed write.
            }
        })
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
