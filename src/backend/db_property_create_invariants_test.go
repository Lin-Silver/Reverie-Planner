package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "testing"
)

func TestDatabasePropertyCreationKeepsSingleNonemptyTitleInvariant(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    if len(database.Properties) != 1 || database.Properties[0].Type != "title" {
        t.Fatalf("new database should start with one title property: %+v", database.Properties)
    }
    title := database.Properties[0]
    providerPath := filepath.Join(app.RootPath, "config", "providers.json")
    before, err := os.ReadFile(providerPath)
    if err != nil { t.Fatal(err) }

    for _, tc := range []struct {
        name string
        payload DBProperty
    }{
        {"empty name", DBProperty{DatabaseID: database.ID, Name: "  \t ", Type: "text", Config: "{}"}},
        {"duplicate title", DBProperty{DatabaseID: database.ID, Name: "Second title", Type: "title", Config: "{}"}},
    } {
        t.Run(tc.name, func(t *testing.T) {
            body, err := json.Marshal(tc.payload)
            if err != nil { t.Fatal(err) }
            res := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(body), app.handleDBProperties)
            if res.Code != http.StatusBadRequest {
                t.Fatalf("invalid property accepted: %d %s", res.Code, res.Body.String())
            }
            var count int
            if err := app.DB.QueryRow("SELECT count(*) FROM db_properties WHERE database_id=?", database.ID).Scan(&count); err != nil || count != 1 {
                t.Fatalf("invalid create changed schema: count=%d err=%v", count, err)
            }
            var name, kind string
            var version int
            if err := app.DB.QueryRow("SELECT name,type,version FROM db_properties WHERE id=?", title.ID).Scan(&name, &kind, &version); err != nil ||
                name != title.Name || kind != "title" || version != title.Version {
                t.Fatalf("required title property changed: name=%q type=%q version=%d err=%v", name, kind, version, err)
            }
            saved, err := app.getDBRecord(record.ID)
            if err != nil || saved.Title != record.Title || saved.Version != record.Version {
                t.Fatalf("failed property create changed record: %+v err=%v", saved, err)
            }
        })
    }
    body, err := json.Marshal(DBProperty{DatabaseID: database.ID, Name: "  Optional notes  ", Type: "text", Config: "{}"})
    if err != nil { t.Fatal(err) }
    res := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(body), app.handleDBProperties)
    if res.Code != http.StatusCreated {
        t.Fatalf("valid property create failed: %d %s", res.Code, res.Body.String())
    }
    var added DBProperty
    if err := json.Unmarshal(res.Body.Bytes(), &added); err != nil { t.Fatal(err) }
    if added.DatabaseID != database.ID || added.Name != "Optional notes" || added.Type != "text" || added.Version != 1 {
        t.Fatalf("valid property not normalized/persisted: %+v", added)
    }
    var titles int
    if err := app.DB.QueryRow("SELECT count(*) FROM db_properties WHERE database_id=? AND type='title' AND deleted_at IS NULL", database.ID).Scan(&titles); err != nil || titles != 1 {
        t.Fatalf("schema has %d title properties: %v", titles, err)
    }
    after, err := os.ReadFile(providerPath)
    if err != nil || !bytes.Equal(before, after) {
        t.Fatalf("property creation altered provider config: %v", err)
    }
}
