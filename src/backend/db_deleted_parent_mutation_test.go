package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// A trashed database's child schema must be preserved for a later restore.
// A direct API DELETE must not mutate its properties or views while the
// owning database is unavailable, even for legacy versionless callers.
func TestDBChildDeleteRejectsTrashedParentAndPreservesRestoreData(t *testing.T) {
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

	for _, tc := range []struct {
		name, table, id, endpoint string
		version                  int
		handler                  http.HandlerFunc
	}{
		{"property", "db_properties", property.ID, "/api/db/properties", property.Version, app.handleDBProperties},
		{"view", "db_views", view.ID, "/api/db/views", view.Version, app.handleDBViews},
	} {
		for _, suffix := range []string{"", fmt.Sprintf("&version=%d", tc.version)} {
			t.Run(tc.name+suffix, func(t *testing.T) {
				response := dbRegressionCall(t, app, http.MethodDelete,
					tc.endpoint+"?id="+tc.id+suffix, "", tc.handler)
				if response.Code != http.StatusNotFound {
					t.Fatalf("child delete after parent trash: got %d, want 404: %s", response.Code, response.Body.String())
				}
				var version int
				var deleted sql.NullString
				err := app.DB.QueryRow(fmt.Sprintf("SELECT version,deleted_at FROM %s WHERE id=?", tc.table), tc.id).
					Scan(&version, &deleted)
				if err != nil {
					t.Fatal(err)
				}
				if version != tc.version || deleted.Valid {
					t.Fatalf("rejected delete mutated %s: version=%d deleted=%v", tc.name, version, deleted)
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
	}

	var deletedDatabase sql.NullString
	if err := app.DB.QueryRow("SELECT deleted_at FROM databases WHERE id=?", database.ID).
		Scan(&deletedDatabase); err != nil {
		t.Fatal(err)
	}
	if !deletedDatabase.Valid {
		t.Fatal("child delete unexpectedly restored its parent database")
	}
}
