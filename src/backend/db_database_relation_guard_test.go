package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"testing"
)

func TestDatabaseDeleteRestrictsExternalRelationSchemas(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, from := dbRegressionCreate(t, app)
	target, to := dbRegressionCreate(t, app)
	property, _ := createIncomingRelationForDeleteTest(t, app, source, target, from, to)
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	assertPreserved := func() {
		t.Helper()
		var version int
		var deleted sql.NullString
		if err := app.DB.QueryRow("SELECT version,deleted_at FROM databases WHERE id=?", target.ID).Scan(&version, &deleted); err != nil {
			t.Fatal(err)
		}
		if version != target.Version || deleted.Valid {
			t.Fatalf("rejected delete changed database: version=%d deleted=%v", version, deleted)
		}
		var pageVersion int
		if err := app.DB.QueryRow("SELECT version FROM pages WHERE id=?", target.PageID).Scan(&pageVersion); err != nil {
			t.Fatal(err)
		}
		if pageVersion != 1 {
			t.Fatalf("rejected delete changed backing page version: %d", pageVersion)
		}
		var relationValue string
		if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?", from.ID, property.ID).Scan(&relationValue); err != nil {
			t.Fatal(err)
		}
		if relationValue != fmt.Sprintf(`[%q]`, to.ID) {
			t.Fatalf("rejected delete changed saved relation: %s", relationValue)
		}
		assertViewIntegrityProviders(t, providerPath, providerBefore)
	}

	for _, suffix := range []string{"", fmt.Sprintf("&version=%d", target.Version)} {
		response := dbRegressionCall(t, app, http.MethodDelete,
			"/api/databases?id="+target.ID+suffix, "", app.handleDatabases)
		if response.Code != http.StatusConflict {
			t.Fatalf("external relation must block target delete (%q): %d %s", suffix, response.Code, response.Body.String())
		}
		assertPreserved()
	}

	// Removing the schema dependency, not just clearing its values, makes the
	// target database safe to trash and restore without dangling relation types.
	removed := dbRegressionCall(t, app, http.MethodDelete,
		"/api/db/properties?id="+property.ID, "", app.handleDBProperties)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("trash relation property: %d %s", removed.Code, removed.Body.String())
	}
	deleted := dbRegressionCall(t, app, http.MethodDelete,
		"/api/databases?id="+target.ID+fmt.Sprintf("&version=%d", target.Version), "", app.handleDatabases)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete after removing dependency: %d %s", deleted.Code, deleted.Body.String())
	}
	restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
		fmt.Sprintf(`{"kind":"database","id":%q}`, target.ID), app.handleDBTrash)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore target database: %d %s", restored.Code, restored.Body.String())
	}
	restoredProperty := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
		fmt.Sprintf(`{"kind":"property","id":%q}`, property.ID), app.handleDBTrash)
	if restoredProperty.Code != http.StatusOK {
		t.Fatalf("restore relation property: %d %s", restoredProperty.Code, restoredProperty.Body.String())
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDatabaseDeleteProtectsTrashedSourceRelationsButAllowsSelfRelations(t *testing.T) {
	t.Run("trashed source database", func(t *testing.T) {
		app := setupTestContext(t)
		defer app.DB.Close()
		source, from := dbRegressionCreate(t, app)
		target, to := dbRegressionCreate(t, app)
		property, _ := createIncomingRelationForDeleteTest(t, app, source, target, from, to)
		providerPath, providerBefore := viewIntegrityProviders(t, app)
		trashedSource := dbRegressionCall(t, app, http.MethodDelete,
			"/api/databases?id="+source.ID, "", app.handleDatabases)
		if trashedSource.Code != http.StatusNoContent {
			t.Fatalf("trash source database: %d %s", trashedSource.Code, trashedSource.Body.String())
		}
		blocked := dbRegressionCall(t, app, http.MethodDelete,
			"/api/databases?id="+target.ID, "", app.handleDatabases)
		if blocked.Code != http.StatusConflict {
			t.Fatalf("trashed source relation must block target deletion: %d %s", blocked.Code, blocked.Body.String())
		}
		var version int
		var deleted sql.NullString
		if err := app.DB.QueryRow("SELECT version,deleted_at FROM databases WHERE id=?", target.ID).Scan(&version, &deleted); err != nil {
			t.Fatal(err)
		}
		if version != target.Version || deleted.Valid {
			t.Fatalf("blocked target was changed: version=%d deleted=%v", version, deleted)
		}
		assertViewIntegrityProviders(t, providerPath, providerBefore)
		restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
			fmt.Sprintf(`{"kind":"database","id":%q}`, source.ID), app.handleDBTrash)
		if restored.Code != http.StatusOK {
			t.Fatalf("restore source database: %d %s", restored.Code, restored.Body.String())
		}
		removed := dbRegressionCall(t, app, http.MethodDelete,
			"/api/db/properties?id="+property.ID, "", app.handleDBProperties)
		if removed.Code != http.StatusNoContent {
			t.Fatalf("remove relation dependency: %d %s", removed.Code, removed.Body.String())
		}
		deletedTarget := dbRegressionCall(t, app, http.MethodDelete,
			"/api/databases?id="+target.ID, "", app.handleDatabases)
		if deletedTarget.Code != http.StatusNoContent {
			t.Fatalf("delete target after dependency removal: %d %s", deletedTarget.Code, deletedTarget.Body.String())
		}
		assertViewIntegrityProviders(t, providerPath, providerBefore)
	})
	t.Run("self relation", func(t *testing.T) {
		app := setupTestContext(t)
		defer app.DB.Close()
		db, record := dbRegressionCreate(t, app)
		createIncomingRelationForDeleteTest(t, app, db, db, record, record)
		providerPath, providerBefore := viewIntegrityProviders(t, app)
		deleted := dbRegressionCall(t, app, http.MethodDelete,
			"/api/databases?id="+db.ID, "", app.handleDatabases)
		if deleted.Code != http.StatusNoContent {
			t.Fatalf("self-relation must not block trashing its own database: %d %s", deleted.Code, deleted.Body.String())
		}
		restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
			fmt.Sprintf(`{"kind":"database","id":%q}`, db.ID), app.handleDBTrash)
		if restored.Code != http.StatusOK {
			t.Fatalf("restore self-related database: %d %s", restored.Code, restored.Body.String())
		}
		assertViewIntegrityProviders(t, providerPath, providerBefore)
	})
}

func TestDatabaseDeleteRelationInspectionFaultRollsBack(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, from := dbRegressionCreate(t, app)
	target, to := dbRegressionCreate(t, app)
	createIncomingRelationForDeleteTest(t, app, source, target, from, to)
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	if _, err := app.DB.Exec("ALTER TABLE db_properties RENAME COLUMN config TO unavailable_config"); err != nil {
		t.Fatal(err)
	}
	failed := dbRegressionCall(t, app, http.MethodDelete,
		"/api/databases?id="+target.ID, "", app.handleDatabases)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("relation inspection fault must fail closed: %d %s", failed.Code, failed.Body.String())
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
		t.Fatalf("SQLite inspection fault changed target: version=%d deleted=%v", version, deleted)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
