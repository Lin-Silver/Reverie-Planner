package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Views of a trashed database are part of its restorable schema. A direct
// view DELETE must not change that schema while the parent is unavailable,
// including when an older client omits the optional version.
func TestDBViewDeleteRejectsTrashedParentAndPreservesRestoreData(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	database, record := dbRegressionCreate(t, app)
	property := computedProperty(t, app, database.ID, "Keep after restore", "text", map[string]any{})
	created := dbRegressionCall(t, app, http.MethodPost, "/api/db/views",
		fmt.Sprintf(`{"databaseId":%q,"name":"Keep view","type":"table","config":"{}"}`, database.ID), app.handleDBViews)
	if created.Code != http.StatusCreated {
		t.Fatalf("create second view: %d %s", created.Code, created.Body.String())
	}
	var view DBView
	if err := json.Unmarshal(created.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	trashed := dbRegressionCall(t, app, http.MethodDelete,
		fmt.Sprintf("/api/databases?id=%s&version=%d", database.ID, database.Version),
		"", app.handleDatabases)
	if trashed.Code != http.StatusNoContent {
		t.Fatalf("trash database: %d %s", trashed.Code, trashed.Body.String())
	}

	for _, suffix := range []string{"", fmt.Sprintf("&version=%d", view.Version)} {
		t.Run("delete view"+suffix, func(t *testing.T) {
			response := dbRegressionCall(t, app, http.MethodDelete,
				"/api/db/views?id="+view.ID+suffix, "", app.handleDBViews)
			if response.Code != http.StatusNotFound {
				t.Fatalf("view delete after parent trash: got %d, want 404: %s", response.Code, response.Body.String())
			}
			var version int
			var deleted sql.NullString
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_views WHERE id=?", view.ID).
				Scan(&version, &deleted); err != nil {
				t.Fatal(err)
			}
			if version != view.Version || deleted.Valid {
				t.Fatalf("rejected delete mutated view: version=%d deleted=%v", version, deleted)
			}
			var propertyVersion int
			var propertyDeleted sql.NullString
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_properties WHERE id=?", property.ID).
				Scan(&propertyVersion, &propertyDeleted); err != nil {
				t.Fatal(err)
			}
			if propertyVersion != property.Version || propertyDeleted.Valid {
				t.Fatalf("rejected delete mutated property: version=%d deleted=%v", propertyVersion, propertyDeleted)
			}
			var recordVersion int
			var recordDeleted sql.NullString
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?", record.ID).
				Scan(&recordVersion, &recordDeleted); err != nil {
				t.Fatal(err)
			}
			if recordVersion != record.Version || recordDeleted.Valid {
				t.Fatalf("rejected delete mutated original record: version=%d deleted=%v", recordVersion, recordDeleted)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}

	var deletedDatabase sql.NullString
	if err := app.DB.QueryRow("SELECT deleted_at FROM databases WHERE id=?", database.ID).
		Scan(&deletedDatabase); err != nil {
		t.Fatal(err)
	}
	if !deletedDatabase.Valid {
		t.Fatal("view delete unexpectedly restored its parent database")
	}
}

// Independent properties, like views, must remain available for a later
// database restore. Preserve the computed-dependency 409 precedence: this
// test covers properties that have no active computed dependents.
func TestDBPropertyDeleteRejectsTrashedParentWithoutDependencies(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	database, record := dbRegressionCreate(t, app)
	property := computedProperty(t, app, database.ID, "Retain independent property", "text", map[string]any{})
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	trashed := dbRegressionCall(t, app, http.MethodDelete,
		fmt.Sprintf("/api/databases?id=%s&version=%d", database.ID, database.Version),
		"", app.handleDatabases)
	if trashed.Code != http.StatusNoContent {
		t.Fatalf("trash database: %d %s", trashed.Code, trashed.Body.String())
	}

	for _, tc := range []struct {
		name, suffix string
	}{
		{"legacy versionless", ""},
		{"matching version", fmt.Sprintf("&version=%d", property.Version)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := dbRegressionCall(t, app, http.MethodDelete,
				"/api/db/properties?id="+property.ID+tc.suffix, "", app.handleDBProperties)
			if response.Code != http.StatusNotFound {
				t.Fatalf("property delete after parent trash: got %d, want 404: %s", response.Code, response.Body.String())
			}
			var version int
			var deleted sql.NullString
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_properties WHERE id=?", property.ID).
				Scan(&version, &deleted); err != nil {
				t.Fatal(err)
			}
			if version != property.Version || deleted.Valid {
				t.Fatalf("rejected delete changed property: version=%d deleted=%v", version, deleted)
			}
			var recordVersion int
			var recordDeleted sql.NullString
			if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?", record.ID).
				Scan(&recordVersion, &recordDeleted); err != nil {
				t.Fatal(err)
			}
			if recordVersion != record.Version || recordDeleted.Valid {
				t.Fatalf("rejected delete changed record: version=%d deleted=%v", recordVersion, recordDeleted)
			}
			var databaseDeleted sql.NullString
			if err := app.DB.QueryRow("SELECT deleted_at FROM databases WHERE id=?", database.ID).
				Scan(&databaseDeleted); err != nil {
				t.Fatal(err)
			}
			if !databaseDeleted.Valid {
				t.Fatal("rejected property deletion unexpectedly restored database")
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}
}
