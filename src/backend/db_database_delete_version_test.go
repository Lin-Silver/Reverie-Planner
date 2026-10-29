package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Database deletion is a soft delete: a stale browser must not hide a newer
// database rename or mutate its backing page, records or provider settings.
func TestDatabaseDeleteVersionConflictPreservesRemoteRename(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	original, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	rename := dbRegressionCall(t, app, http.MethodPut, "/api/databases",
		fmt.Sprintf(`{"id":%q,"version":%d,"title":"Renamed remotely","icon":%q}`,
			original.ID, original.Version, original.Icon), app.handleDatabases)
	if rename.Code != http.StatusOK {
		t.Fatalf("rename database: %d %s", rename.Code, rename.Body.String())
	}
	var current Database
	if err := json.Unmarshal(rename.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Version != original.Version+1 {
		t.Fatalf("rename did not increment version: before=%d after=%d", original.Version, current.Version)
	}

	path := "/api/databases?id=" + original.ID
	for _, tc := range []struct {
		name, suffix string
		want int
	}{
		{"malformed version", "&version=not-a-number", http.StatusBadRequest},
		{"zero version", "&version=0", http.StatusBadRequest},
		{"stale version", fmt.Sprintf("&version=%d", original.Version), http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := dbRegressionCall(t, app, http.MethodDelete, path+tc.suffix, "", app.handleDatabases)
			if response.Code != tc.want {
				t.Fatalf("delete returned %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
			var title string
			var version int
			var deleted sql.NullString
			if err := app.DB.QueryRow("SELECT title,version,deleted_at FROM databases WHERE id=?", original.ID).
				Scan(&title, &version, &deleted); err != nil {
				t.Fatal(err)
			}
			if title != current.Title || version != current.Version || deleted.Valid {
				t.Fatalf("rejected delete changed database: title=%q version=%d deleted=%v", title, version, deleted)
			}
			var pageTitle string
			if err := app.DB.QueryRow("SELECT title FROM pages WHERE id=?", original.PageID).Scan(&pageTitle); err != nil {
				t.Fatal(err)
			}
			if pageTitle != current.Title {
				t.Fatalf("rejected delete changed backing page title: %q", pageTitle)
			}
			if _, err := app.getDBRecord(record.ID); err != nil {
				t.Fatalf("rejected delete lost record: %v", err)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}

	response := dbRegressionCall(t, app, http.MethodDelete,
		path+fmt.Sprintf("&version=%d", current.Version), "", app.handleDatabases)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete with latest version: %d %s", response.Code, response.Body.String())
	}
	var version int
	var deleted sql.NullString
	if err := app.DB.QueryRow("SELECT version,deleted_at FROM databases WHERE id=?", original.ID).
		Scan(&version, &deleted); err != nil {
		t.Fatal(err)
	}
	if !deleted.Valid || version != current.Version+1 {
		t.Fatalf("latest-version delete must tombstone once: version=%d deleted=%v", version, deleted)
	}
	var recordDeleted sql.NullString
	if err := app.DB.QueryRow("SELECT deleted_at FROM db_records WHERE id=?", record.ID).
		Scan(&recordDeleted); err != nil {
		t.Fatal(err)
	}
	if recordDeleted.Valid {
		t.Fatal("deleting a database unexpectedly tombstoned its record")
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDatabaseDeleteLegacyCompatibilityAndMissingID(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	original, _ := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	for _, path := range []string{"/api/databases", "/api/databases?id=missing_database"} {
		response := dbRegressionCall(t, app, http.MethodDelete, path, "", app.handleDatabases)
		want := http.StatusNotFound
		if path == "/api/databases" {
			want = http.StatusBadRequest
		}
		if response.Code != want {
			t.Fatalf("%s returned %d, want %d: %s", path, response.Code, want, response.Body.String())
		}
	}
	response := dbRegressionCall(t, app, http.MethodDelete, "/api/databases?id="+original.ID, "", app.handleDatabases)
	if response.Code != http.StatusNoContent {
		t.Fatalf("legacy versionless delete: %d %s", response.Code, response.Body.String())
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
