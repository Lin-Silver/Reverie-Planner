package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func propertyIntegrityCreate(t *testing.T, app *AppContext, databaseID, name string) DBProperty {
	t.Helper()
	payload := fmt.Sprintf(`{"databaseId":%q,"name":%q,"type":"text","config":"{}"}`, databaseID, name)
	response := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", payload, app.handleDBProperties)
	if response.Code != http.StatusCreated {
		t.Fatalf("create property %q: %d %s", name, response.Code, response.Body.String())
	}
	var property DBProperty
	if err := json.Unmarshal(response.Body.Bytes(), &property); err != nil {
		t.Fatal(err)
	}
	return property
}

func TestDBPropertyCreateRejectsPositionOverflowWithoutWriting(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	title := database.Properties[0]

	// max-int64 + 1 becomes SQLite REAL and cannot be scanned into int64.
	// Ignoring the scan error used to create a property at position zero.
	if _, err := app.DB.Exec("UPDATE db_properties SET position=? WHERE id=?", int64(9223372036854775807), title.ID); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"databaseId":%q,"name":"Must not exist","type":"text","config":"{}"}`, database.ID)
	failed := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", payload, app.handleDBProperties)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("position overflow should fail closed with 500, got %d: %s", failed.Code, failed.Body.String())
	}
	var count int
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", database.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed create changed property count: %d, %v", count, err)
	}
	var position int64
	var version int
	if err := app.DB.QueryRow("SELECT position,version FROM db_properties WHERE id=?", title.ID).Scan(&position, &version); err != nil {
		t.Fatal(err)
	}
	if position != 9223372036854775807 || version != title.Version {
		t.Fatalf("failed create modified original property: position=%d version=%d", position, version)
	}
	saved, err := app.getDBRecord(record.ID)
	if err != nil || saved.Title != record.Title || saved.Version != record.Version {
		t.Fatalf("failed create modified record: %+v, %v", saved, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	if _, err := app.DB.Exec("UPDATE db_properties SET position=0 WHERE id=?", title.ID); err != nil {
		t.Fatal(err)
	}
	recovered := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", payload, app.handleDBProperties)
	if recovered.Code != http.StatusCreated {
		t.Fatalf("valid creation after fault: %d %s", recovered.Code, recovered.Body.String())
	}
	var created DBProperty
	if err := json.Unmarshal(recovered.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Position != 1 || created.Version != 1 {
		t.Fatalf("recovered property has incorrect position/version: %+v", created)
	}
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", database.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("valid create did not persist: count=%d err=%v", count, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBPropertyDeleteDistinguishesMissingFromSQLiteFailure(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	property := propertyIntegrityCreate(t, app, database.ID, "Deletable")

	missing := dbRegressionCall(t, app, http.MethodDelete, "/api/db/properties?id=missing-property", "", app.handleDBProperties)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing property should be 404, got %d: %s", missing.Code, missing.Body.String())
	}
	empty := dbRegressionCall(t, app, http.MethodDelete, "/api/db/properties", "", app.handleDBProperties)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty property ID should be 400, got %d: %s", empty.Code, empty.Body.String())
	}
	if _, err := app.DB.Exec("ALTER TABLE db_properties RENAME TO db_properties_unavailable"); err != nil {
		t.Fatal(err)
	}
	failed := dbRegressionCall(t, app, http.MethodDelete, "/api/db/properties?id="+property.ID, "", app.handleDBProperties)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("SQLite lookup failure should be 500, got %d: %s", failed.Code, failed.Body.String())
	}
	if _, err := app.DB.Exec("ALTER TABLE db_properties_unavailable RENAME TO db_properties"); err != nil {
		t.Fatal(err)
	}
	var tombstone *string
	var version int
	if err := app.DB.QueryRow("SELECT deleted_at,version FROM db_properties WHERE id=?", property.ID).Scan(&tombstone, &version); err != nil {
		t.Fatal(err)
	}
	if tombstone != nil || version != property.Version {
		t.Fatalf("failed delete changed tombstone/version: %v %d", tombstone, version)
	}
	saved, err := app.getDBRecord(record.ID)
	if err != nil || saved.Title != record.Title || saved.Version != record.Version {
		t.Fatalf("failed delete changed record: %+v %v", saved, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	deleted := dbRegressionCall(t, app, http.MethodDelete, "/api/db/properties?id="+property.ID, "", app.handleDBProperties)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("valid delete after fault: %d %s", deleted.Code, deleted.Body.String())
	}
	if err := app.DB.QueryRow("SELECT deleted_at,version FROM db_properties WHERE id=?", property.ID).Scan(&tombstone, &version); err != nil {
		t.Fatal(err)
	}
	if tombstone == nil || version != property.Version+1 {
		t.Fatalf("valid delete did not persist: tombstone=%v version=%d", tombstone, version)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBPropertyCreationReservesTrashedPositionForRestore(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, _ := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	first := propertyIntegrityCreate(t, app, database.ID, "First")
	if first.Position != 1 {
		t.Fatalf("unexpected first property position: %+v", first)
	}
	deleted := dbRegressionCall(t, app, http.MethodDelete, "/api/db/properties?id="+first.ID, "", app.handleDBProperties)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete first property: %d %s", deleted.Code, deleted.Body.String())
	}
	second := propertyIntegrityCreate(t, app, database.ID, "Second")
	if second.Position != 2 || second.ID == first.ID {
		t.Fatalf("deleted property position reused: first=%+v second=%+v", first, second)
	}
	restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
		fmt.Sprintf(`{"kind":"property","id":%q}`, first.ID), app.handleDBTrash)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore first property: %d %s", restored.Code, restored.Body.String())
	}
	var restoredPosition, restoredVersion int
	if err := app.DB.QueryRow("SELECT position,version FROM db_properties WHERE id=? AND deleted_at IS NULL", first.ID).Scan(&restoredPosition, &restoredVersion); err != nil {
		t.Fatal(err)
	}
	if restoredPosition != 1 || restoredVersion != first.Version+2 {
		t.Fatalf("restored property position/version changed: %d %d", restoredPosition, restoredVersion)
	}
	var active int
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=? AND deleted_at IS NULL", database.ID).Scan(&active); err != nil || active != 3 {
		t.Fatalf("restore lost properties: active=%d err=%v", active, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
