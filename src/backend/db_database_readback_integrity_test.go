package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// A successful database UPDATE followed by an unreadable response used to
// commit the rename and page-version bump before returning HTTP 500.
func TestDatabaseUpdateReadbackFailureRollsBackDatabaseAndPage(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	var originalPageTitle string
	var originalPageVersion int
	if err := app.DB.QueryRow("SELECT title, version FROM pages WHERE id=?", database.PageID).
		Scan(&originalPageTitle, &originalPageVersion); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"id":%q,"version":%d,"title":"Renamed database","icon":"book"}`,
		database.ID, database.Version)

	// SQLite accepts TEXT in an INTEGER-affinity version column. The
	// update succeeds, but reading the version into an int must fail.
	if _, err := app.DB.Exec(`CREATE TRIGGER corrupt_database_readback AFTER UPDATE ON databases
		WHEN NEW.id = '` + database.ID + `'
		BEGIN UPDATE databases SET version='unscannable-version' WHERE id=NEW.id; END`); err != nil {
		t.Fatal(err)
	}
	failed := dbRegressionCall(t, app, http.MethodPut, "/api/databases", payload, app.handleDatabases)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("readback fault should reject rename with 500: %d %s", failed.Code, failed.Body.String())
	}
	if _, err := app.DB.Exec("DROP TRIGGER corrupt_database_readback"); err != nil {
		t.Fatal(err)
	}
	assertDatabaseRenameRollback(t, app, database, record, originalPageTitle, originalPageVersion)
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	recovered := dbRegressionCall(t, app, http.MethodPut, "/api/databases", payload, app.handleDatabases)
	if recovered.Code != http.StatusOK {
		t.Fatalf("retry after readback repair: %d %s", recovered.Code, recovered.Body.String())
	}
	var updated Database
	if err := json.Unmarshal(recovered.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != database.ID || updated.Title != "Renamed database" ||
		updated.Icon != "book" || updated.Version != database.Version+1 ||
		len(updated.Properties) != len(database.Properties) || len(updated.Views) != len(database.Views) {
		t.Fatalf("unexpected successful database response: %+v", updated)
	}
	var pageTitle string
	var pageVersion int
	if err := app.DB.QueryRow("SELECT title, version FROM pages WHERE id=?", database.PageID).
		Scan(&pageTitle, &pageVersion); err != nil {
		t.Fatal(err)
	}
	if pageTitle != updated.Title || pageVersion != originalPageVersion+1 {
		t.Fatalf("backing page not updated exactly once: %q v%d", pageTitle, pageVersion)
	}
	stale := dbRegressionCall(t, app, http.MethodPut, "/api/databases", payload, app.handleDatabases)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale retry should conflict: %d %s", stale.Code, stale.Body.String())
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

// Even a failure to list nested views must not commit a database rename.
func TestDatabaseUpdateNestedReadbackFailureRollsBack(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	var pageTitle string
	var pageVersion int
	if err := app.DB.QueryRow("SELECT title, version FROM pages WHERE id=?", database.PageID).
		Scan(&pageTitle, &pageVersion); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"id":%q,"version":%d,"title":"Should roll back","icon":"nested"}`,
		database.ID, database.Version)
	if _, err := app.DB.Exec("ALTER TABLE db_views RENAME TO db_views_unavailable"); err != nil {
		t.Fatal(err)
	}
	failed := dbRegressionCall(t, app, http.MethodPut, "/api/databases", payload, app.handleDatabases)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("nested readback fault should be 500: %d %s", failed.Code, failed.Body.String())
	}
	if _, err := app.DB.Exec("ALTER TABLE db_views_unavailable RENAME TO db_views"); err != nil {
		t.Fatal(err)
	}
	assertDatabaseRenameRollback(t, app, database, record, pageTitle, pageVersion)
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	recovered := dbRegressionCall(t, app, http.MethodPut, "/api/databases", payload, app.handleDatabases)
	if recovered.Code != http.StatusOK {
		t.Fatalf("rename after restoring views: %d %s", recovered.Code, recovered.Body.String())
	}
	var updated Database
	if err := json.Unmarshal(recovered.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Should roll back" || updated.Version != database.Version+1 ||
		len(updated.Properties) != len(database.Properties) || len(updated.Views) != len(database.Views) {
		t.Fatalf("incomplete successful response: %+v", updated)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func assertDatabaseRenameRollback(t *testing.T, app *AppContext, original Database, record DBRecord, pageTitle string, pageVersion int) {
	t.Helper()
	current, err := app.getDatabase(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Title != original.Title || current.Icon != original.Icon ||
		current.Version != original.Version || len(current.Properties) != len(original.Properties) ||
		len(current.Views) != len(original.Views) {
		t.Fatalf("rejected rename changed database: %+v", current)
	}
	var savedPageTitle string
	var savedPageVersion int
	if err := app.DB.QueryRow("SELECT title, version FROM pages WHERE id=?", original.PageID).
		Scan(&savedPageTitle, &savedPageVersion); err != nil {
		t.Fatal(err)
	}
	if savedPageTitle != pageTitle || savedPageVersion != pageVersion {
		t.Fatalf("rejected rename changed backing page: %q v%d", savedPageTitle, savedPageVersion)
	}
	savedRecord, err := app.getDBRecord(record.ID)
	if err != nil || savedRecord.Title != record.Title || savedRecord.Version != record.Version {
		t.Fatalf("rejected rename changed record: %+v %v", savedRecord, err)
	}
}
