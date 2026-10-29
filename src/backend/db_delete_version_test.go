package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseOptionalDeleteVersion(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		want int
		invalid bool
	}{
		{"legacy omitted", "", 0, false},
		{"valid", "?version=7", 7, false},
		{"empty", "?version=", 0, true},
		{"bare key", "?version", 0, true},
		{"duplicate matching", "?version=7&version=7", 0, true},
		{"duplicate conflicting", "?version=7&version=8", 0, true},
		{"duplicate empty", "?version=&version=7", 0, true},
		{"zero", "?version=0", 0, true},
		{"negative", "?version=-1", 0, true},
		{"not numeric", "?version=abc", 0, true},
		{"space", "?version=%20", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/databases"+tc.query, nil)
			got, err := parseOptionalDeleteVersion(req)
			if (err != nil) != tc.invalid || (!tc.invalid && got != tc.want) {
				t.Fatalf("version=%d error=%v, want version=%d invalid=%v", got, err, tc.want, tc.invalid)
			}
		})
	}
}

// Rejecting an ambiguous version must not fall back to a versionless delete.
// This exercises each actual handler, not just the shared parser.
func TestDBDeleteAmbiguousVersionPreservesDataAndProviders(t *testing.T) {
	for _, suffix := range []string{"&version=", "&version=1&version=1", "&version=1&version=2"} {
		t.Run(suffix, func(t *testing.T) {
			app := setupTestContext(t)
			defer app.DB.Close()
			database, record := dbRegressionCreate(t, app)
			property := computedProperty(t, app, database.ID, "Delete guard", "text", map[string]any{})
			providerPath, providerBefore := viewIntegrityProviders(t, app)

			for _, tc := range []struct {
				name, table, id string
				version int
				handler http.HandlerFunc
				path string
			}{
				{"database", "databases", database.ID, database.Version, app.handleDatabases, "/api/databases"},
				{"record", "db_records", record.ID, record.Version, app.handleDBRecords, "/api/db/records"},
				{"property", "db_properties", property.ID, property.Version, app.handleDBProperties, "/api/db/properties"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					response := dbRegressionCall(t, app, http.MethodDelete, tc.path+"?id="+tc.id+suffix, "", tc.handler)
					if response.Code != http.StatusBadRequest {
						t.Fatalf("ambiguous delete returned %d: %s", response.Code, response.Body.String())
					}
					var version int
					var deleted sql.NullString
					err := app.DB.QueryRow(fmt.Sprintf("SELECT version, deleted_at FROM %s WHERE id=?", tc.table), tc.id).
						Scan(&version, &deleted)
					if err != nil {
						t.Fatal(err)
					}
					if version != tc.version || deleted.Valid {
						t.Fatalf("rejected delete changed %s: version=%d deleted=%v", tc.name, version, deleted)
					}
					assertViewIntegrityProviders(t, providerPath, providerBefore)
				})
			}
		})
	}
}
