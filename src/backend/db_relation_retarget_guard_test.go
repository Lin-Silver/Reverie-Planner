package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRelationTargetChangePreservesLiveAndTrashedReferences(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	source, live := dbRegressionCreate(t, app)
	target, targetRecord := dbRegressionCreate(t, app)
	other, _ := dbRegressionCreate(t, app)
	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil { t.Fatal(err) }

	config := func(databaseID string) string {
		data, err := json.Marshal(map[string]string{"targetDatabaseId": databaseID})
		if err != nil { t.Fatal(err) }
		return string(data)
	}
	createBody, err := json.Marshal(DBProperty{
		DatabaseID: source.ID, Name: "Related", Type: "relation", Config: config(target.ID),
	})
	if err != nil { t.Fatal(err) }
	created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(createBody), app.handleDBProperties)
	if created.Code != http.StatusCreated { t.Fatalf("create relation: %d %s", created.Code, created.Body.String()) }
	var property DBProperty
	if err := json.Unmarshal(created.Body.Bytes(), &property); err != nil { t.Fatal(err) }

	createdRecord := dbRegressionCall(t, app, http.MethodPost, "/api/db/records",
		`{"databaseId":"`+source.ID+`","title":"Restore later"}`, app.handleDBRecords)
	if createdRecord.Code != http.StatusCreated { t.Fatalf("create second record: %d %s", createdRecord.Code, createdRecord.Body.String()) }
	var trashed DBRecord
	if err := json.Unmarshal(createdRecord.Body.Bytes(), &trashed); err != nil { t.Fatal(err) }

	save := func(recordID string, version int, ids []string) int {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"id": recordID, "version": version,
			"values": map[string]any{property.ID: ids},
		})
		if err != nil { t.Fatal(err) }
		res := dbRegressionCall(t, app, http.MethodPut, "/api/db/records", string(body), app.handleDBRecords)
		if res.Code != http.StatusOK { t.Fatalf("save relation: %d %s", res.Code, res.Body.String()) }
		var updated DBRecord
		if err := json.Unmarshal(res.Body.Bytes(), &updated); err != nil { t.Fatal(err) }
		return updated.Version
	}
	live.Version = save(live.ID, live.Version, []string{targetRecord.ID})
	trashed.Version = save(trashed.ID, trashed.Version, []string{targetRecord.ID})
	removed := dbRegressionCall(t, app, http.MethodDelete, "/api/db/records?id="+trashed.ID, "", app.handleDBRecords)
	if removed.Code != http.StatusNoContent { t.Fatalf("trash record: %d %s", removed.Code, removed.Body.String()) }

	attempt := func(want int) *DBProperty {
		t.Helper()
		changed := property
		changed.Config = config(other.ID)
		payload, err := json.Marshal(changed)
		if err != nil { t.Fatal(err) }
		res := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(payload), app.handleDBProperties)
		if res.Code != want { t.Fatalf("retarget: want %d, got %d: %s", want, res.Code, res.Body.String()) }
		if want != http.StatusOK { return nil }
		var updated DBProperty
		if err := json.Unmarshal(res.Body.Bytes(), &updated); err != nil { t.Fatal(err) }
		return &updated
	}
	assertPreserved := func(liveValue, trashedValue string) {
		t.Helper()
		var gotConfig string
		var gotVersion int
		if err := app.DB.QueryRow("SELECT config,version FROM db_properties WHERE id=?", property.ID).
			Scan(&gotConfig, &gotVersion); err != nil { t.Fatal(err) }
		if gotConfig != property.Config || gotVersion != property.Version {
			t.Fatalf("rejected retarget mutated schema: %q version %d", gotConfig, gotVersion)
		}
		for _, entry := range []struct{id, want string}{{live.ID, liveValue}, {trashed.ID, trashedValue}} {
			var raw string
			if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
				entry.id, property.ID).Scan(&raw); err != nil { t.Fatal(err) }
			if raw != entry.want { t.Fatalf("rejected retarget changed value for %s: %s", entry.id, raw) }
		}
		var deleted sql.NullString
		if err := app.DB.QueryRow("SELECT deleted_at FROM db_records WHERE id=?", trashed.ID).Scan(&deleted); err != nil { t.Fatal(err) }
		if !deleted.Valid { t.Fatal("rejected retarget unexpectedly restored deleted record") }
	}
	populated := `["` + targetRecord.ID + `"]`
	attempt(http.StatusConflict)
	assertPreserved(populated, populated)

	// Even after clearing the live record, the deleted record retains a
	// reference that must not be silently reinterpreted on restoration.
	live.Version = save(live.ID, live.Version, []string{})
	attempt(http.StatusConflict)
	assertPreserved("[]", populated)

	// A real SQLite read failure must fail closed, not appear compatible.
	if _, err := app.DB.Exec("ALTER TABLE db_values RENAME COLUMN value TO unavailable_value"); err != nil { t.Fatal(err) }
	attempt(http.StatusInternalServerError)
	if _, err := app.DB.Exec("ALTER TABLE db_values RENAME COLUMN unavailable_value TO value"); err != nil { t.Fatal(err) }
	assertPreserved("[]", populated)

	restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
		`{"kind":"record","id":"`+trashed.ID+`"}`, app.handleDBTrash)
	if restored.Code != http.StatusOK { t.Fatalf("restore record: %d %s", restored.Code, restored.Body.String()) }
	if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", trashed.ID).Scan(&trashed.Version); err != nil { t.Fatal(err) }
	trashed.Version = save(trashed.ID, trashed.Version, []string{})

	updated := attempt(http.StatusOK)
	if updated.Config != config(other.ID) || updated.Version != property.Version+1 {
		t.Fatalf("empty relation values should allow retarget: %+v", updated)
	}
	for _, id := range []string{live.ID, trashed.ID} {
		var raw string
		if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?", id, property.ID).Scan(&raw); err != nil { t.Fatal(err) }
		if raw != "[]" { t.Fatalf("retarget changed empty relation value for %s: %s", id, raw) }
	}
	providerAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, providerAfter) {
		t.Fatalf("relation retarget changed AI Provider configuration: %v", err)
	}
}
