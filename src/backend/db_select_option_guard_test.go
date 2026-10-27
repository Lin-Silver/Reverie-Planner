package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "testing"
)

func TestSelectOptionChangesProtectLiveAndTrashedRecordValues(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    db, live := dbRegressionCreate(t, app)

    providerPath := filepath.Join(app.RootPath, "config", "providers.json")
    providersBefore, err := os.ReadFile(providerPath)
    if err != nil { t.Fatal(err) }

    createProperty := func(name, kind, config string) DBProperty {
        t.Helper()
        payload, err := json.Marshal(DBProperty{DatabaseID: db.ID, Name: name, Type: kind, Config: config})
        if err != nil { t.Fatal(err) }
        res := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(payload), app.handleDBProperties)
        if res.Code != http.StatusCreated { t.Fatalf("create %s: %d %s", kind, res.Code, res.Body.String()) }
        var prop DBProperty
        if err := json.Unmarshal(res.Body.Bytes(), &prop); err != nil { t.Fatal(err) }
        return prop
    }
    status := createProperty("Status", "select", `{"options":["Todo","Done"]}`)
    tags := createProperty("Tags", "multi_select", `{"options":["Red","Blue"]}`)

    create := dbRegressionCall(t, app, http.MethodPost, "/api/db/records",
        `{"databaseId":"`+db.ID+`","title":"Trashed later"}`, app.handleDBRecords)
    if create.Code != http.StatusCreated { t.Fatalf("create second record: %d %s", create.Code, create.Body.String()) }
    var trashed DBRecord
    if err := json.Unmarshal(create.Body.Bytes(), &trashed); err != nil { t.Fatal(err) }

    saveValues := func(record DBRecord, selected string, colors []string) {
        t.Helper()
        payload, err := json.Marshal(map[string]any{
            "id": record.ID, "version": record.Version, "title": record.Title,
            "values": map[string]any{status.ID: selected, tags.ID: colors},
        })
        if err != nil { t.Fatal(err) }
        res := dbRegressionCall(t, app, http.MethodPut, "/api/db/records", string(payload), app.handleDBRecords)
        if res.Code != http.StatusOK { t.Fatalf("save record values: %d %s", res.Code, res.Body.String()) }
    }
    saveValues(live, "Todo", []string{"Red"})
    saveValues(trashed, "Done", []string{"Blue"})
    removed := dbRegressionCall(t, app, http.MethodDelete, "/api/db/records?id="+trashed.ID, "", app.handleDBRecords)
    if removed.Code != http.StatusNoContent { t.Fatalf("trash record: %d %s", removed.Code, removed.Body.String()) }

    checkState := func(prop DBProperty, wantConfig string, wantVersion int) {
        t.Helper()
        var config string
        var version int
        if err := app.DB.QueryRow("SELECT config,version FROM db_properties WHERE id=?", prop.ID).Scan(&config, &version); err != nil { t.Fatal(err) }
        if config != wantConfig || version != wantVersion {
            t.Fatalf("schema changed unexpectedly: config=%q version=%d, want %q/%d", config, version, wantConfig, wantVersion)
        }
        for _, rec := range []struct{recordID, propID, want string}{
            {live.ID, status.ID, `"Todo"`}, {trashed.ID, status.ID, `"Done"`},
            {live.ID, tags.ID, `["Red"]`}, {trashed.ID, tags.ID, `["Blue"]`},
        } {
            var raw string
            if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?", rec.recordID, rec.propID).Scan(&raw); err != nil { t.Fatal(err) }
            if raw != rec.want { t.Fatalf("stored value %s/%s changed to %s", rec.recordID, rec.propID, raw) }
        }
        for _, recID := range []string{live.ID, trashed.ID} {
            var version int
            if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", recID).Scan(&version); err != nil { t.Fatal(err) }
            wantVersion := 2 // One successful value update.
            if recID == trashed.ID { wantVersion++ } // Soft deletion also versions the record.
            if version != wantVersion { t.Fatalf("property edit changed record %s version to %d, want %d", recID, version, wantVersion) }
        }
    }
    update := func(prop DBProperty, config string, wantStatus int) DBProperty {
        t.Helper()
        prop.Config = config
        payload, err := json.Marshal(prop)
        if err != nil { t.Fatal(err) }
        res := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(payload), app.handleDBProperties)
        if res.Code != wantStatus { t.Fatalf("update %s: expected %d, got %d %s", prop.Name, wantStatus, res.Code, res.Body.String()) }
        if wantStatus != http.StatusOK { return prop }
        var saved DBProperty
        if err := json.Unmarshal(res.Body.Bytes(), &saved); err != nil { t.Fatal(err) }
        return saved
    }

    update(status, `{"options":["Done"]}`, http.StatusConflict)
    checkState(status, status.Config, status.Version)
    update(status, `{"options":["Todo"]}`, http.StatusConflict) // Deleted record still uses Done.
    checkState(status, status.Config, status.Version)
    update(tags, `{"options":["Red"]}`, http.StatusConflict) // Deleted record still uses Blue.
    checkState(tags, tags.Config, tags.Version)

    initialStatus := status
    status = update(status, `{"options":["Todo","Done","Later"]}`, http.StatusOK)
    if status.Version != initialStatus.Version+1 { t.Fatalf("successful option edit did not advance version: %+v", status) }
    checkState(status, status.Config, status.Version)
    update(initialStatus, `{"options":["Todo","Done","Old"]}`, http.StatusConflict)
    checkState(status, status.Config, status.Version)

    // Removing an unused option is safe; the stored record values stay intact.
    status = update(status, `{"options":["Todo","Done"]}`, http.StatusOK)
    checkState(status, status.Config, status.Version)

    // A SQLite read failure must not masquerade as a valid schema edit.
    if _, err := app.DB.Exec("ALTER TABLE db_values RENAME COLUMN value TO unavailable_value"); err != nil { t.Fatal(err) }
    update(status, `{"options":["Todo","Done","New"]}`, http.StatusInternalServerError)
    if _, err := app.DB.Exec("ALTER TABLE db_values RENAME COLUMN unavailable_value TO value"); err != nil { t.Fatal(err) }
    checkState(status, status.Config, status.Version)
    status = update(status, `{"options":["Todo","Done","New"]}`, http.StatusOK)
    checkState(status, status.Config, status.Version)

    providersAfter, err := os.ReadFile(providerPath)
    if err != nil || !bytes.Equal(providersBefore, providersAfter) {
        t.Fatalf("property schema edits modified AI Provider configuration: %v", err)
    }
}
