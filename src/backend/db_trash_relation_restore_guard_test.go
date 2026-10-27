package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTrashRelationPropertyRestoreRejectsDeletedReferencedRecords(t *testing.T) {
	for _, sourceDeleted := range []bool{false, true} {
		name := "active source"
		if sourceDeleted {
			name = "trashed source"
		}
		t.Run(name, func(t *testing.T) {
			app := setupTestContext(t)
			defer app.DB.Close()
			source, from := dbRegressionCreate(t, app)
			target, to := dbRegressionCreate(t, app)
			providerPath, providerBefore := viewIntegrityProviders(t, app)

			config, err := json.Marshal(map[string]string{"targetDatabaseId": target.ID})
			if err != nil {
				t.Fatal(err)
			}
			propertyBody, err := json.Marshal(DBProperty{
				DatabaseID: source.ID, Name: "Related", Type: "relation", Config: string(config),
			})
			if err != nil {
				t.Fatal(err)
			}
			created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties",
				string(propertyBody), app.handleDBProperties)
			if created.Code != http.StatusCreated {
				t.Fatalf("create relation: %d %s", created.Code, created.Body.String())
			}
			var property DBProperty
			if err := json.Unmarshal(created.Body.Bytes(), &property); err != nil {
				t.Fatal(err)
			}
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

			if sourceDeleted {
				removed := dbRegressionCall(t, app, http.MethodDelete,
					"/api/db/records?id="+from.ID, "", app.handleDBRecords)
				if removed.Code != http.StatusNoContent {
					t.Fatalf("trash source: %d %s", removed.Code, removed.Body.String())
				}
			}
			removedProperty := dbRegressionCall(t, app, http.MethodDelete,
				"/api/db/properties?id="+property.ID, "", app.handleDBProperties)
			if removedProperty.Code != http.StatusNoContent {
				t.Fatalf("trash property: %d %s", removedProperty.Code, removedProperty.Body.String())
			}
			removedTarget := dbRegressionCall(t, app, http.MethodDelete,
				"/api/db/records?id="+to.ID, "", app.handleDBRecords)
			if removedTarget.Code != http.StatusNoContent {
				t.Fatalf("trash target record: %d %s", removedTarget.Code, removedTarget.Body.String())
			}

			var beforeVersion int
			var beforeDeleted, beforeValue string
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_properties WHERE id=?",
				property.ID).Scan(&beforeVersion, &beforeDeleted); err != nil {
				t.Fatal(err)
			}
			if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
				from.ID, property.ID).Scan(&beforeValue); err != nil {
				t.Fatal(err)
			}
			restorePropertyBody, err := json.Marshal(map[string]string{"kind": "property", "id": property.ID})
			if err != nil {
				t.Fatal(err)
			}
			restoreTargetBody, err := json.Marshal(map[string]string{"kind": "record", "id": to.ID})
			if err != nil {
				t.Fatal(err)
			}
			restoreSourceBody, err := json.Marshal(map[string]string{"kind": "record", "id": from.ID})
			if err != nil {
				t.Fatal(err)
			}
			attempt := func(want int) {
				t.Helper()
				res := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
					string(restorePropertyBody), app.handleDBTrash)
				if res.Code != want {
					t.Fatalf("restore property: want %d got %d: %s", want, res.Code, res.Body.String())
				}
			}
			assertPreserved := func() {
				t.Helper()
				var version int
				var deletedAt, raw string
				if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_properties WHERE id=?",
					property.ID).Scan(&version, &deletedAt); err != nil {
					t.Fatal(err)
				}
				if version != beforeVersion || deletedAt != beforeDeleted {
					t.Fatalf("failed restore changed property tombstone: %d %q", version, deletedAt)
				}
				if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
					from.ID, property.ID).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				if raw != beforeValue {
					t.Fatalf("failed restore changed saved relation: %s", raw)
				}
				assertViewIntegrityProviders(t, providerPath, providerBefore)
			}

			attempt(http.StatusConflict)
			assertPreserved()

			// Invalid legacy values cannot be reactivated as valid relations.
			if _, err := app.DB.Exec("UPDATE db_values SET value=? WHERE record_id=? AND property_id=?",
				"{\"invalid\":true}", from.ID, property.ID); err != nil {
				t.Fatal(err)
			}
			attempt(http.StatusConflict)
			if _, err := app.DB.Exec("UPDATE db_values SET value=? WHERE record_id=? AND property_id=?",
				beforeValue, from.ID, property.ID); err != nil {
				t.Fatal(err)
			}
			assertPreserved()

			// An actual SQLite read fault is a 500, not a missing-record conflict.
			if _, err := app.DB.Exec("ALTER TABLE db_values RENAME COLUMN value TO unavailable_value"); err != nil {
				t.Fatal(err)
			}
			attempt(http.StatusInternalServerError)
			if _, err := app.DB.Exec("ALTER TABLE db_values RENAME COLUMN unavailable_value TO value"); err != nil {
				t.Fatal(err)
			}
			assertPreserved()

			restoredTarget := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
				string(restoreTargetBody), app.handleDBTrash)
			if restoredTarget.Code != http.StatusOK {
				t.Fatalf("restore target record: %d %s", restoredTarget.Code, restoredTarget.Body.String())
			}
			attempt(http.StatusOK)
			if sourceDeleted {
				restoredSource := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
					string(restoreSourceBody), app.handleDBTrash)
				if restoredSource.Code != http.StatusOK {
					t.Fatalf("restore source record: %d %s", restoredSource.Code, restoredSource.Body.String())
				}
			}
			var afterVersion int
			if err := app.DB.QueryRow("SELECT version FROM db_properties WHERE id=?",
				property.ID).Scan(&afterVersion); err != nil {
				t.Fatal(err)
			}
			if afterVersion != beforeVersion+1 {
				t.Fatalf("successful restore must increment version once: %d -> %d", beforeVersion, afterVersion)
			}
			record, err := app.getDBRecord(from.ID)
			if err != nil {
				t.Fatal(err)
			}
			value, ok := record.Values[property.ID].([]any)
			if !ok || len(value) != 1 || value[0] != to.ID {
				t.Fatalf("restored relation lost original target: %#v", record.Values[property.ID])
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}
}
