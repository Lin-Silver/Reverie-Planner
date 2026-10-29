package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Legacy database files can contain configurations written before duplicate-key
// validation existed. Restore must not silently select the last relation target.
func TestTrashRestoreRejectsAmbiguousSavedRelationTargets(t *testing.T) {
	for _, kind := range []string{"record", "property"} {
		t.Run(kind, func(t *testing.T) {
			app := setupTestContext(t)
			defer app.DB.Close()

			source, from := dbRegressionCreate(t, app)
			target, to := dbRegressionCreate(t, app)
			providerPath, providerBefore := viewIntegrityProviders(t, app)
			originalConfig := `{"targetDatabaseId":"` + target.ID + `"}`
			ambiguousConfig := `{"targetDatabaseId":"` + target.ID + `","targetDatabaseId":"` + source.ID + `"}`

			createBody, err := json.Marshal(DBProperty{
				DatabaseID: source.ID, Name: "Related", Type: "relation", Config: originalConfig,
			})
			if err != nil {
				t.Fatal(err)
			}
			created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties",
				string(createBody), app.handleDBProperties)
			if created.Code != http.StatusCreated {
				t.Fatalf("create relation: %d %s", created.Code, created.Body.String())
			}
			var property DBProperty
			if err := json.Unmarshal(created.Body.Bytes(), &property); err != nil {
				t.Fatal(err)
			}

			id := property.ID
			if kind == "record" {
				saveBody, err := json.Marshal(map[string]any{
					"id": from.ID, "version": from.Version,
					"values": map[string]any{property.ID: []string{to.ID}},
				})
				if err != nil {
					t.Fatal(err)
				}
				saved := dbRegressionCall(t, app, http.MethodPut, "/api/db/records",
					string(saveBody), app.handleDBRecords)
				if saved.Code != http.StatusOK {
					t.Fatalf("save relation: %d %s", saved.Code, saved.Body.String())
				}
				id = from.ID
			}

			table, deletePath, deleteHandler := "db_properties", "/api/db/properties?id="+id, app.handleDBProperties
			if kind == "record" {
				table, deletePath, deleteHandler = "db_records", "/api/db/records?id="+id, app.handleDBRecords
			}
			deleted := dbRegressionCall(t, app, http.MethodDelete, deletePath, "", deleteHandler)
			if deleted.Code != http.StatusNoContent {
				t.Fatalf("trash %s: %d %s", kind, deleted.Code, deleted.Body.String())
			}

			if _, err := app.DB.Exec("UPDATE db_properties SET config=? WHERE id=?",
				ambiguousConfig, property.ID); err != nil {
				t.Fatal(err)
			}
			var version int
			var deletedAt string
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM "+table+" WHERE id=?",
				id).Scan(&version, &deletedAt); err != nil {
				t.Fatal(err)
			}
			var originalValue string
			if kind == "record" {
				if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
					from.ID, property.ID).Scan(&originalValue); err != nil {
					t.Fatal(err)
				}
			}
			payload, err := json.Marshal(map[string]string{"kind": kind, "id": id})
			if err != nil {
				t.Fatal(err)
			}
			restore := func(want int) {
				t.Helper()
				res := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
					string(payload), app.handleDBTrash)
				if res.Code != want {
					t.Fatalf("restore %s: want %d, got %d: %s", kind, want, res.Code, res.Body.String())
				}
			}
			restore(http.StatusConflict)

			var actualVersion int
			var actualDeleted, actualConfig string
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM "+table+" WHERE id=?",
				id).Scan(&actualVersion, &actualDeleted); err != nil {
				t.Fatal(err)
			}
			if actualVersion != version || actualDeleted != deletedAt {
				t.Fatalf("rejected restore modified %s tombstone/version", kind)
			}
			if err := app.DB.QueryRow("SELECT config FROM db_properties WHERE id=?",
				property.ID).Scan(&actualConfig); err != nil {
				t.Fatal(err)
			}
			if actualConfig != ambiguousConfig {
				t.Fatalf("rejected restore rewrote legacy configuration: %q", actualConfig)
			}
			if kind == "record" {
				var actualValue string
				if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
					from.ID, property.ID).Scan(&actualValue); err != nil {
					t.Fatal(err)
				}
				if actualValue != originalValue {
					t.Fatalf("rejected restore changed relation value: %q", actualValue)
				}
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)

			if _, err := app.DB.Exec("UPDATE db_properties SET config=? WHERE id=?",
				originalConfig, property.ID); err != nil {
				t.Fatal(err)
			}
			restore(http.StatusOK)
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM "+table+" WHERE id=?",
				id).Scan(&actualVersion, &actualDeleted); err != nil {
				t.Fatal(err)
			}
			if actualVersion != version+1 || actualDeleted != "" {
				t.Fatalf("repaired restore did not reactivate %s correctly: version=%d deleted=%q",
					kind, actualVersion, actualDeleted)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}
}
