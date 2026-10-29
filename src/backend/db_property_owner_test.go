package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "testing"
)

func TestPropertyUpdatePreservesOwnerAndInvalidDefinitionRollback(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()

    source, _ := dbRegressionCreate(t, app)
    other, _ := dbRegressionCreate(t, app)

    createProperty := func(databaseID, name, kind, config string) DBProperty {
        t.Helper()
        request, err := json.Marshal(DBProperty{DatabaseID: databaseID, Name: name, Type: kind, Config: config})
        if err != nil { t.Fatal(err) }
        response := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(request), app.handleDBProperties)
        if response.Code != http.StatusCreated { t.Fatalf("create %s property: %d %s", kind, response.Code, response.Body.String()) }
        var property DBProperty
        if err := json.Unmarshal(response.Body.Bytes(), &property); err != nil { t.Fatal(err) }
        return property
    }

    sendUpdate := func(property DBProperty) *DBProperty {
        t.Helper()
        request, err := json.Marshal(property)
        if err != nil { t.Fatal(err) }
        response := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(request), app.handleDBProperties)
        if response.Code != http.StatusOK {
            t.Fatalf("valid update failed: %d %s", response.Code, response.Body.String())
        }
        var updated DBProperty
        if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil { t.Fatal(err) }
        return &updated
    }

    expectRejectedUnchanged := func(label string, attempt, original DBProperty) {
        t.Helper()
        request, err := json.Marshal(attempt)
        if err != nil { t.Fatal(err) }
        response := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(request), app.handleDBProperties)
        if response.Code != http.StatusBadRequest {
            t.Fatalf("%s: expected 400, got %d: %s", label, response.Code, response.Body.String())
        }
        var name, kind, config, owner string
        var version, visible int
        if err := app.DB.QueryRow(`SELECT name,type,config,database_id,version,visible FROM db_properties WHERE id=?`,
            original.ID).Scan(&name, &kind, &config, &owner, &version, &visible); err != nil {
            t.Fatal(err)
        }
        if name != original.Name || kind != original.Type || config != original.Config ||
            owner != original.DatabaseID || version != original.Version || (visible == 1) != original.Visible {
            t.Fatalf("%s: failed update changed property: name=%q type=%q config=%q owner=%q version=%d visible=%d, before=%+v",
                label, name, kind, config, owner, version, visible, original)
        }
    }

    providersPath := filepath.Join(app.RootPath, "config", "providers.json")
    providersBefore, err := os.ReadFile(providersPath)
    if err != nil { t.Fatal(err) }

    property := createProperty(source.ID, "Safe", "text", "{}")
    forged := property
    forged.DatabaseID = other.ID
    forged.Name = "Cross-database mutation"
    expectRejectedUnchanged("foreign databaseId cannot rename another property", forged, property)

    empty := property
    empty.Name = "  \t "
    expectRejectedUnchanged("empty name cannot replace typed property", empty, property)

    title := source.Properties[0]
    emptyTitle := title
    emptyTitle.Name = "  "
    expectRejectedUnchanged("empty title name", emptyTitle, title)

    otherNumber := createProperty(other.ID, "Other number", "number", "{}")
    formula := createProperty(source.ID, "Safe formula", "formula", `{"operation":"sum","args":[{"number":1}]}`)
    forgedFormula := formula
    forgedFormula.DatabaseID = other.ID
    forgedFormula.Config = `{"operation":"sum","args":[{"propertyId":"` + otherNumber.ID + `"}]}`
    expectRejectedUnchanged("foreign formula dependency cannot be validated against wrong owner", forgedFormula, formula)

    relation := createProperty(source.ID, "Related", "relation", `{"targetDatabaseId":"`+other.ID+`"}`)
    removed := dbRegressionCall(t, app, http.MethodDelete, "/api/databases?id="+other.ID, "", app.handleDatabases)
    if removed.Code != http.StatusNoContent { t.Fatalf("trash relation target: %d %s", removed.Code, removed.Body.String()) }
    renamedRelation := relation
    renamedRelation.Name = "Invalid relation to trashed database"
    expectRejectedUnchanged("trashed relation target cannot be saved", renamedRelation, relation)

    valid := property
    valid.Name = "Renamed safely"
    saved := sendUpdate(valid)
    if saved.DatabaseID != source.ID || saved.Name != valid.Name || saved.Version != property.Version + 1 {
        t.Fatalf("valid owner-scoped edit did not persist correctly: %+v", saved)
    }
    var otherTitle string
    if err := app.DB.QueryRow("SELECT title FROM databases WHERE id=?", other.ID).Scan(&otherTitle); err != nil || otherTitle != other.Title {
        t.Fatalf("failed property writes touched unrelated database: %q %v", otherTitle, err)
    }
    afterProviders, err := os.ReadFile(providersPath)
    if err != nil || !bytes.Equal(providersBefore, afterProviders) {
        t.Fatalf("property validation modified AI provider configuration: %v", err)
    }
}
