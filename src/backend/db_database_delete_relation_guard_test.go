package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func createDatabaseDeleteRelation(t *testing.T, app *AppContext, sourceID, targetID string) DBProperty {
	t.Helper()
	config := fmt.Sprintf(`{"targetDatabaseId":%q}`, targetID)
	body := fmt.Sprintf(`{"databaseId":%q,"name":"Target","type":"relation","config":%q}`, sourceID, config)
	response := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", body, app.handleDBProperties)
	if response.Code != http.StatusCreated {
		t.Fatalf("create relation property: %d %s", response.Code, response.Body.String())
	}
	var property DBProperty
	if err := json.Unmarshal(response.Body.Bytes(), &property); err != nil {
		t.Fatal(err)
	}
	return property
}

func TestDatabaseDeleteRestrictsExternalRelationDefinitionsIncludingTrashedSources(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, _ := dbRegressionCreate(t, app)
	target, targetRecord := dbRegressionCreate(t, app)
	property := createDatabaseDeleteRelation(t, app, source.ID, target.ID)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	assertUnchanged := func() {
		t.Helper()
		var version, pageVersion, recordVersion, propertyVersion int
		var deleted sql.NullString
		if err := app.DB.QueryRow("SELECT version,deleted_at FROM databases WHERE id=?", target.ID).Scan(&version, &deleted); err != nil {
			t.Fatal(err)
		}
		if version != target.Version || deleted.Valid {
			t.Fatalf("rejected database deletion changed target: version=%d deleted=%v", version, deleted)
		}
		if err := app.DB.QueryRow("SELECT version FROM pages WHERE id=?", target.PageID).Scan(&pageVersion); err != nil {
			t.Fatal(err)
		}
		if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", targetRecord.ID).Scan(&recordVersion); err != nil {
			t.Fatal(err)
		}
		if err := app.DB.QueryRow("SELECT version FROM db_properties WHERE id=?", property.ID).Scan(&propertyVersion); err != nil {
			t.Fatal(err)
		}
		if pageVersion != 1 || recordVersion != targetRecord.Version || propertyVersion != property.Version {
			t.Fatalf("rejected deletion changed page/record/property: %d %d %d", pageVersion, recordVersion, propertyVersion)
		}
		assertViewIntegrityProviders(t, providerPath, providerBefore)
	}
	path := "/api/databases?id=" + target.ID
	blocked := dbRegressionCall(t, app, http.MethodDelete, path, "", app.handleDatabases)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("unused external relation definition must restrict delete: %d %s", blocked.Code, blocked.Body.String())
	}
	assertUnchanged()
	trashedSource := dbRegressionCall(t, app, http.MethodDelete, "/api/databases?id="+source.ID, "", app.handleDatabases)
	if trashedSource.Code != http.StatusNoContent {
		t.Fatalf("trash source database: %d %s", trashedSource.Code, trashedSource.Body.String())
	}
	blocked = dbRegressionCall(t, app, http.MethodDelete, path, "", app.handleDatabases)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("trashed source's active relation definition must restrict delete: %d %s", blocked.Code, blocked.Body.String())
	}
	assertUnchanged()
	removedProperty := dbRegressionCall(t, app, http.MethodDelete, "/api/db/properties?id="+property.ID, "", app.handleDBProperties)
	if removedProperty.Code != http.StatusNoContent {
		t.Fatalf("trash relation property: %d %s", removedProperty.Code, removedProperty.Body.String())
	}
	deleted := dbRegressionCall(t, app, http.MethodDelete, path, "", app.handleDatabases)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete after relation property removal: %d %s", deleted.Code, deleted.Body.String())
	}
	var version int
	var tombstone sql.NullString
	if err := app.DB.QueryRow("SELECT version,deleted_at FROM databases WHERE id=?", target.ID).Scan(&version, &tombstone); err != nil {
		t.Fatal(err)
	}
	if version != target.Version+1 || !tombstone.Valid {
		t.Fatalf("database tombstone not persisted: %d %v", version, tombstone)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDatabaseDeleteAllowsInternalRelationsAndFailsClosedOnSQLiteFaults(t *testing.T) {
	t.Run("internal relation", func(t *testing.T) {
		app := setupTestContext(t)
		defer app.DB.Close()
		database, _ := dbRegressionCreate(t, app)
		createDatabaseDeleteRelation(t, app, database.ID, database.ID)
		providerPath, providerBefore := viewIntegrityProviders(t, app)
		response := dbRegressionCall(t, app, http.MethodDelete, "/api/databases?id="+database.ID, "", app.handleDatabases)
		if response.Code != http.StatusNoContent {
			t.Fatalf("internal relation should not block owner deletion: %d %s", response.Code, response.Body.String())
		}
		assertViewIntegrityProviders(t, providerPath, providerBefore)
	})
	t.Run("sqlite read fault", func(t *testing.T) {
		app := setupTestContext(t)
		defer app.DB.Close()
		source, _ := dbRegressionCreate(t, app)
		target, _ := dbRegressionCreate(t, app)
		property := createDatabaseDeleteRelation(t, app, source.ID, target.ID)
		providerPath, providerBefore := viewIntegrityProviders(t, app)
		for _, tc := range []struct{ path string; want int }{
			{"/api/databases", http.StatusBadRequest},
			{"/api/databases?id=missing-database", http.StatusNotFound},
		} {
			response := dbRegressionCall(t, app, http.MethodDelete, tc.path, "", app.handleDatabases)
			if response.Code != tc.want {
				t.Fatalf("invalid delete %q: got %d want %d: %s", tc.path, response.Code, tc.want, response.Body.String())
			}
		}
		if _, err := app.DB.Exec("ALTER TABLE db_properties RENAME COLUMN config TO unavailable_config"); err != nil {
			t.Fatal(err)
		}
		failed := dbRegressionCall(t, app, http.MethodDelete, "/api/databases?id="+target.ID, "", app.handleDatabases)
		if failed.Code != http.StatusInternalServerError {
			t.Fatalf("SQLite dependency read fault must return 500: %d %s", failed.Code, failed.Body.String())
		}
		if _, err := app.DB.Exec("ALTER TABLE db_properties RENAME COLUMN unavailable_config TO config"); err != nil {
			t.Fatal(err)
		}
		var version int
		var deleted sql.NullString
		if err := app.DB.QueryRow("SELECT version,deleted_at FROM databases WHERE id=?", target.ID).Scan(&version, &deleted); err != nil {
			t.Fatal(err)
		}
		if version != target.Version || deleted.Valid {
			t.Fatalf("faulted delete changed target: version=%d deleted=%v", version, deleted)
		}
		assertViewIntegrityProviders(t, providerPath, providerBefore)
		removed := dbRegressionCall(t, app, http.MethodDelete, "/api/db/properties?id="+property.ID, "", app.handleDBProperties)
		if removed.Code != http.StatusNoContent {
			t.Fatalf("remove relation after fault repair: %d %s", removed.Code, removed.Body.String())
		}
		deletedResponse := dbRegressionCall(t, app, http.MethodDelete, "/api/databases?id="+target.ID, "", app.handleDatabases)
		if deletedResponse.Code != http.StatusNoContent {
			t.Fatalf("delete after repair: %d %s", deletedResponse.Code, deletedResponse.Body.String())
		}
		assertViewIntegrityProviders(t, providerPath, providerBefore)
	})
}
