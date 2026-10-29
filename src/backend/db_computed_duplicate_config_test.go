package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

// Computed configuration must not depend on encoding/json's last-key-wins
// behavior: a duplicate operand, operation or target can change persisted
// calculations without an intentional schema edit.
func TestComputedConfigurationsRejectDuplicateKeysWithoutMutation(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()

    source, record := dbRegressionCreate(t, app)
    target, _ := dbRegressionCreate(t, app)
    price := computedProperty(t, app, target.ID, "Price", "number", map[string]any{})
    relation := computedProperty(t, app, source.ID, "Related", "relation", map[string]any{
        "targetDatabaseId": target.ID,
    })
    formula := computedProperty(t, app, source.ID, "Total", "formula", map[string]any{
        "operation": "sum",
        "args": []any{map[string]any{"number": float64(1)}},
    })
    rollup := computedProperty(t, app, source.ID, "Rollup", "rollup", map[string]any{
        "relationPropertyId": relation.ID,
        "targetPropertyId": price.ID,
        "aggregate": "sum",
    })
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    cases := []struct {
        name string
        saved DBProperty
        invalid string
    }{
        {
            name: "formula operation",
            saved: formula,
            invalid: `{"operation":"sum","operation":"concat","args":[{"number":1}]}`,
        },
        {
            name: "formula nested operand",
            saved: formula,
            invalid: `{"operation":"sum","args":[{"number":1,"number":2}]}`,
        },
        {
            name: "rollup aggregate",
            saved: rollup,
            invalid: fmt.Sprintf(`{"relationPropertyId":%q,"targetPropertyId":%q,"aggregate":"sum","aggregate":"count"}`, relation.ID, price.ID),
        },
        {
            name: "rollup nested metadata",
            saved: rollup,
            invalid: fmt.Sprintf(`{"relationPropertyId":%q,"targetPropertyId":%q,"aggregate":"sum","metadata":{"key":1,"key":2}}`, relation.ID, price.ID),
        },
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if tc.saved.Type == "formula" {
                if _, err := parseFormulaDefinition(tc.invalid); err == nil {
                    t.Fatal("ambiguous formula configuration parsed successfully")
                }
            } else {
                if _, err := parseRollupDefinition(tc.invalid); err == nil {
                    t.Fatal("ambiguous rollup configuration parsed successfully")
                }
            }

            var countBefore int
            if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", source.ID).Scan(&countBefore); err != nil {
                t.Fatal(err)
            }
            invalidCreate := DBProperty{
                DatabaseID: source.ID,
                Name: "Must not exist",
                Type: tc.saved.Type,
                Config: tc.invalid,
            }
            createBody, err := json.Marshal(invalidCreate)
            if err != nil { t.Fatal(err) }
            created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(createBody), app.handleDBProperties)
            if created.Code != http.StatusBadRequest {
                t.Fatalf("ambiguous property creation: %d %s", created.Code, created.Body.String())
            }
            var countAfter int
            if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", source.ID).Scan(&countAfter); err != nil {
                t.Fatal(err)
            }
            if countAfter != countBefore {
                t.Fatalf("rejected create changed property count: %d -> %d", countBefore, countAfter)
            }

            invalidUpdate := tc.saved
            invalidUpdate.Name = "Must not rename"
            invalidUpdate.Config = tc.invalid
            updateBody, err := json.Marshal(invalidUpdate)
            if err != nil { t.Fatal(err) }
            updated := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(updateBody), app.handleDBProperties)
            if updated.Code != http.StatusBadRequest {
                t.Fatalf("ambiguous property update: %d %s", updated.Code, updated.Body.String())
            }
            var name, config string
            var version int
            if err := app.DB.QueryRow("SELECT name,config,version FROM db_properties WHERE id=?", tc.saved.ID).
                Scan(&name, &config, &version); err != nil { t.Fatal(err) }
            if name != tc.saved.Name || config != tc.saved.Config || version != tc.saved.Version {
                t.Fatalf("rejected update mutated computed schema: name=%q config=%q version=%d", name, config, version)
            }
            var recordVersion int
            if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", record.ID).Scan(&recordVersion); err != nil { t.Fatal(err) }
            if recordVersion != record.Version {
                t.Fatalf("rejected schema mutation changed record version: %d -> %d", record.Version, recordVersion)
            }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
        })
    }

    // The guard must not prevent ordinary unambiguous computed definitions.
    for _, saved := range []DBProperty{formula, rollup} {
        valid := saved
        valid.Name += " updated"
        body, err := json.Marshal(valid)
        if err != nil { t.Fatal(err) }
        response := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(body), app.handleDBProperties)
        if response.Code != http.StatusOK {
            t.Fatalf("valid %s update rejected: %d %s", saved.Type, response.Code, response.Body.String())
        }
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
