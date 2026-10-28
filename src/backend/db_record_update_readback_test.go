package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

// A failed readback must roll back the record version, property values,
// computed-property changes and backing-page title/version together.
func TestDBRecordUpdateReadbackFailureRollsBackAllChanges(t *testing.T) {
    cases := []struct {
        name string
        trigger func(recordID, numberPropertyID, formulaPropertyID string) string
    }{
        {
            name: "unreadable record version",
            trigger: func(recordID, _, _ string) string {
                return fmt.Sprintf(`CREATE TRIGGER corrupt_record_update AFTER UPDATE ON db_records
                    WHEN NEW.id=%q AND NEW.title='Faulted update'
                    BEGIN UPDATE db_records SET version='bad-version' WHERE id=NEW.id; END`, recordID)
            },
        },
        {
            name: "unreadable backing page version",
            trigger: func(_, _, _ string) string {
                return `CREATE TRIGGER corrupt_record_update AFTER UPDATE ON pages
                    WHEN NEW.title='Faulted update'
                    BEGIN UPDATE pages SET version='bad-version' WHERE id=NEW.id; END`
            },
        },
        {
            name: "invalid stored property JSON",
            trigger: func(_, numberPropertyID, _ string) string {
                return fmt.Sprintf(`CREATE TRIGGER corrupt_record_update AFTER INSERT ON db_values
                    WHEN NEW.property_id=%q
                    BEGIN UPDATE db_values SET value='invalid-json' WHERE record_id=NEW.record_id AND property_id=NEW.property_id; END`, numberPropertyID)
            },
        },
        {
            name: "unreadable computed property",
            trigger: func(recordID, _, formulaPropertyID string) string {
                return fmt.Sprintf(`CREATE TRIGGER corrupt_record_update AFTER UPDATE ON db_records
                    WHEN NEW.id=%q AND NEW.title='Faulted update'
                    BEGIN UPDATE db_properties SET config='invalid-formula' WHERE id=%q; END`, recordID, formulaPropertyID)
            },
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            app := setupTestContext(t)
            defer app.DB.Close()
            database, original := dbRegressionCreate(t, app)

            numberPayload := fmt.Sprintf(`{"databaseId":%q,"name":"Amount","type":"number"}`, database.ID)
            numberResult := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", numberPayload, app.handleDBProperties)
            if numberResult.Code != http.StatusCreated {
                t.Fatalf("create number property: %d %s", numberResult.Code, numberResult.Body.String())
            }
            var number DBProperty
            if err := json.Unmarshal(numberResult.Body.Bytes(), &number); err != nil { t.Fatal(err) }

            formulaPayload := fmt.Sprintf(`{"databaseId":%q,"name":"Constant","type":"formula","config":%q}`,
                database.ID, `{"operation":"sum","args":[{"number":1}]}`)
            formulaResult := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", formulaPayload, app.handleDBProperties)
            if formulaResult.Code != http.StatusCreated {
                t.Fatalf("create formula property: %d %s", formulaResult.Code, formulaResult.Body.String())
            }
            var formula DBProperty
            if err := json.Unmarshal(formulaResult.Body.Bytes(), &formula); err != nil { t.Fatal(err) }
            providerPath, providerBefore := viewIntegrityProviders(t, app)

            if _, err := app.DB.Exec(tc.trigger(original.ID, number.ID, formula.ID)); err != nil { t.Fatal(err) }
            payload := fmt.Sprintf(`{"id":%q,"title":"Faulted update","version":%d,"values":{%q:12}}`,
                original.ID, original.Version, number.ID)
            failed := dbRegressionCall(t, app, http.MethodPut, "/api/db/records", payload, app.handleDBRecords)
            if failed.Code != http.StatusInternalServerError {
                t.Fatalf("readback fault must return 500, got %d: %s", failed.Code, failed.Body.String())
            }
            if _, err := app.DB.Exec("DROP TRIGGER corrupt_record_update"); err != nil { t.Fatal(err) }

            saved, err := app.getDBRecord(original.ID)
            if err != nil { t.Fatalf("failed update damaged readable record: %v", err) }
            if saved.Title != original.Title || saved.Version != original.Version ||
                saved.PageID != original.PageID || saved.Values[number.ID] != nil ||
                saved.Values[formula.ID] != float64(1) {
                t.Fatalf("rejected update changed record or values: %+v", saved)
            }
            var pageTitle string
            var pageVersion int
            if err := app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?", original.PageID).
                Scan(&pageTitle, &pageVersion); err != nil { t.Fatal(err) }
            if pageTitle != original.Title || pageVersion != 1 {
                t.Fatalf("rejected update changed page: %q v%d", pageTitle, pageVersion)
            }
            var storedValues int
            if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_values WHERE record_id=? AND property_id=?",
                original.ID, number.ID).Scan(&storedValues); err != nil { t.Fatal(err) }
            if storedValues != 0 { t.Fatalf("failed update inserted %d property values", storedValues) }
            var savedConfig string
            if err := app.DB.QueryRow("SELECT config FROM db_properties WHERE id=?", formula.ID).
                Scan(&savedConfig); err != nil { t.Fatal(err) }
            if savedConfig != formula.Config { t.Fatalf("rejected update changed computed config: %q", savedConfig) }
            assertViewIntegrityProviders(t, providerPath, providerBefore)

            retried := dbRegressionCall(t, app, http.MethodPut, "/api/db/records", payload, app.handleDBRecords)
            if retried.Code != http.StatusOK {
                t.Fatalf("retry after removing fault: %d %s", retried.Code, retried.Body.String())
            }
            var updated DBRecord
            if err := json.Unmarshal(retried.Body.Bytes(), &updated); err != nil { t.Fatal(err) }
            if updated.Title != "Faulted update" || updated.Version != original.Version+1 ||
                updated.PageID != original.PageID || updated.Values[number.ID] != float64(12) ||
                updated.Values[formula.ID] != float64(1) {
                t.Fatalf("incomplete successful response: %+v", updated)
            }
            if err := app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?", original.PageID).
                Scan(&pageTitle, &pageVersion); err != nil { t.Fatal(err) }
            if pageTitle != updated.Title || pageVersion != 2 {
                t.Fatalf("successful retry did not synchronize page: %q v%d", pageTitle, pageVersion)
            }
            persisted, err := app.getDBRecord(original.ID)
            if err != nil || persisted.Version != updated.Version ||
                persisted.Values[number.ID] != updated.Values[number.ID] {
                t.Fatalf("response differs from persisted record: %+v %v", persisted, err)
            }
            stale := dbRegressionCall(t, app, http.MethodPut, "/api/db/records", payload, app.handleDBRecords)
            if stale.Code != http.StatusConflict {
                t.Fatalf("stale retry should return 409: %d %s", stale.Code, stale.Body.String())
            }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
        })
    }
}
