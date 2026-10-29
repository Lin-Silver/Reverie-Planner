package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestDBRecordDeleteVersionConflictPreservesNewerEdit(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	_, original := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	update := fmt.Sprintf(`{"id":%q,"version":%d,"title":"Updated in another tab","values":{}}`, original.ID, original.Version)
	response := dbRegressionCall(t, app, http.MethodPut, "/api/db/records", update, app.handleDBRecords)
	if response.Code != http.StatusOK {
		t.Fatalf("update record: %d %s", response.Code, response.Body.String())
	}
	var current DBRecord
	if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Version != original.Version+1 {
		t.Fatalf("expected newer record version, got %d", current.Version)
	}

	path := "/api/db/records?id=" + original.ID
	for _, tc := range []struct {
		name, suffix string
		want         int
	}{
		{"malformed version", "&version=not-a-number", http.StatusBadRequest},
		{"zero version", "&version=0", http.StatusBadRequest},
		{"stale version", fmt.Sprintf("&version=%d", original.Version), http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failed := dbRegressionCall(t, app, http.MethodDelete, path+tc.suffix, "", app.handleDBRecords)
			if failed.Code != tc.want {
				t.Fatalf("delete: got %d, want %d: %s", failed.Code, tc.want, failed.Body.String())
			}
			saved, err := app.getDBRecord(original.ID)
			if err != nil || saved.Title != current.Title || saved.Version != current.Version {
				t.Fatalf("rejected deletion lost newer edit: %+v %v", saved, err)
			}
			var deleted sql.NullString
			if err := app.DB.QueryRow("SELECT deleted_at FROM db_records WHERE id=?", original.ID).Scan(&deleted); err != nil {
				t.Fatal(err)
			}
			if deleted.Valid {
				t.Fatal("rejected deletion created a tombstone")
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}

	deleted := dbRegressionCall(t, app, http.MethodDelete,
		path+fmt.Sprintf("&version=%d", current.Version), "", app.handleDBRecords)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete after reading latest version: %d %s", deleted.Code, deleted.Body.String())
	}
	var tombstone sql.NullString
	var version int
	if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?", original.ID).Scan(&version, &tombstone); err != nil {
		t.Fatal(err)
	}
	if !tombstone.Valid || version != current.Version+1 {
		t.Fatalf("successful delete must tombstone latest version: version=%d deleted=%v", version, tombstone)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBRecordDeleteWithoutVersionRemainsCompatible(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	_, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	result := dbRegressionCall(t, app, http.MethodDelete, "/api/db/records?id="+record.ID, "", app.handleDBRecords)
	if result.Code != http.StatusNoContent {
		t.Fatalf("legacy versionless DELETE: %d %s", result.Code, result.Body.String())
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
