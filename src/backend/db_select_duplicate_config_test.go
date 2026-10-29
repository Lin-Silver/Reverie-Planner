package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "testing"
)

// Duplicate JSON keys in select definitions must not be interpreted with
// encoding/json's last-key-wins behavior during create or schema updates.
func TestSelectDefinitionsRejectAmbiguousJSONWithoutMutatingData(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    providerPath := filepath.Join(app.RootPath, "config", "providers.json")
    before, err := os.ReadFile(providerPath)
    if err != nil { t.Fatal(err) }

    configs := []string{
        "{\"options\":[\"First\"],\"options\":[\"Second\"]}",
        "{\"options\":[\"First\"],\"metadata\":{\"key\":1,\"key\":2}}",
    }
    for _, kind := range []string{"select", "multi_select"} {
        valid := DBProperty{
            DatabaseID: database.ID, Name: "Options " + kind,
            Type: kind, Config: "{\"options\":[\"First\",\"Second\"]}",
        }
        for _, config := range configs {
            invalid := valid
            invalid.Config = config
            payload, err := json.Marshal(invalid)
            if err != nil { t.Fatal(err) }
            response := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(payload), app.handleDBProperties)
            if response.Code != http.StatusBadRequest {
                t.Fatalf("%s create accepted duplicate JSON keys: %d %s", kind, response.Code, response.Body.String())
            }
            var count int
            if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=? AND name=?", database.ID, valid.Name).Scan(&count); err != nil { t.Fatal(err) }
            if count != 0 { t.Fatalf("%s invalid create inserted %d properties", kind, count) }
        }

        payload, err := json.Marshal(valid)
        if err != nil { t.Fatal(err) }
        created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(payload), app.handleDBProperties)
        if created.Code != http.StatusCreated { t.Fatalf("%s valid create: %d %s", kind, created.Code, created.Body.String()) }
        var saved DBProperty
        if err := json.Unmarshal(created.Body.Bytes(), &saved); err != nil { t.Fatal(err) }

        for _, config := range configs {
            changed := saved
            changed.Config = config
            payload, err := json.Marshal(changed)
            if err != nil { t.Fatal(err) }
            response := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(payload), app.handleDBProperties)
            if response.Code != http.StatusBadRequest {
                t.Fatalf("%s update accepted duplicate JSON keys: %d %s", kind, response.Code, response.Body.String())
            }
            var configAfter string
            var versionAfter int
            if err := app.DB.QueryRow("SELECT config,version FROM db_properties WHERE id=?", saved.ID).Scan(&configAfter, &versionAfter); err != nil { t.Fatal(err) }
            if configAfter != saved.Config || versionAfter != saved.Version {
                t.Fatalf("%s rejected update changed schema: config=%q version=%d", kind, configAfter, versionAfter)
            }
            var recordVersion int
            if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", record.ID).Scan(&recordVersion); err != nil { t.Fatal(err) }
            if recordVersion != record.Version { t.Fatalf("%s rejected update changed record version to %d", kind, recordVersion) }
        }

        changed := saved
        changed.Config = "{\"options\":[\"First\",\"Second\",\"Third\"]}"
        payload, err = json.Marshal(changed)
        if err != nil { t.Fatal(err) }
        response := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(payload), app.handleDBProperties)
        if response.Code != http.StatusOK { t.Fatalf("%s valid update after rejection: %d %s", kind, response.Code, response.Body.String()) }
    }
    after, err := os.ReadFile(providerPath)
    if err != nil || !bytes.Equal(before, after) {
        t.Fatalf("select validation modified AI Provider configuration: %v", err)
    }
}
