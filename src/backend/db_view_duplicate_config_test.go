package main

import (
    "encoding/json"
    "net/http"
    "testing"
)

// A view configuration can contain nested filter/sort/layout JSON. Duplicate
// keys must not be interpreted with encoding/json's last-key-wins behavior.
func TestDBViewConfigRejectsDuplicateKeysWithoutMutatingData(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)
    original := database.Views[0]

    invalidConfigs := []string{
        `{"sortRules":[],"sortRules":[{"propertyId":"other","direction":"desc"}]}`,
        `{"sortRules":[{"propertyId":"field","direction":"asc","direction":"desc"}]}`,
        `{"layout":{"width":100,"width":200}}`,
        `{"filters":[{"conditions":[{"value":"first","value":"second"}]}]}`,
    }

    for _, config := range invalidConfigs {
        t.Run(config, func(t *testing.T) {
            created := DBView{
                DatabaseID: database.ID, Name: "Ambiguous view",
                Type: "table", Config: config,
            }
            payload, err := json.Marshal(created)
            if err != nil { t.Fatal(err) }
            response := dbRegressionCall(t, app, http.MethodPost,
                "/api/db/views", string(payload), app.handleDBViews)
            if response.Code != http.StatusBadRequest {
                t.Fatalf("ambiguous view create returned %d: %s", response.Code, response.Body.String())
            }
            var count int
            if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_views WHERE database_id=?", database.ID).Scan(&count); err != nil {
                t.Fatal(err)
            }
            if count != 1 {
                t.Fatalf("rejected create changed view count: %d", count)
            }

            updated := original
            updated.Name = "Must not replace original"
            updated.Config = config
            payload, err = json.Marshal(updated)
            if err != nil { t.Fatal(err) }
            response = dbRegressionCall(t, app, http.MethodPut,
                "/api/db/views", string(payload), app.handleDBViews)
            if response.Code != http.StatusBadRequest {
                t.Fatalf("ambiguous view update returned %d: %s", response.Code, response.Body.String())
            }
            var name, savedConfig string
            var version int
            if err := app.DB.QueryRow("SELECT name,config,version FROM db_views WHERE id=?", original.ID).
                Scan(&name, &savedConfig, &version); err != nil { t.Fatal(err) }
            if name != original.Name || savedConfig != original.Config || version != original.Version {
                t.Fatalf("rejected update changed view: name=%q config=%q version=%d", name, savedConfig, version)
            }
            saved, err := app.getDBRecord(record.ID)
            if err != nil || saved.Title != record.Title || saved.Version != record.Version {
                t.Fatalf("rejected view mutation changed record: %+v %v", saved, err)
            }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
        })
    }

    // A valid nested configuration must still be accepted after rejections.
    updated := original
    updated.Name = "Valid view"
    updated.Config = `{"sortRules":[],"layout":{"width":200}}`
    payload, err := json.Marshal(updated)
    if err != nil { t.Fatal(err) }
    response := dbRegressionCall(t, app, http.MethodPut,
        "/api/db/views", string(payload), app.handleDBViews)
    if response.Code != http.StatusOK {
        t.Fatalf("valid view update after rejection: %d %s", response.Code, response.Body.String())
    }
    var saved DBView
    if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil { t.Fatal(err) }
    if saved.Config != updated.Config || saved.Name != updated.Name || saved.Version != original.Version+1 {
        t.Fatalf("valid view update was not persisted: %+v", saved)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
