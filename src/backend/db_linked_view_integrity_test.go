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

func linkedViewTestPage(t *testing.T, app *AppContext) Page {
	t.Helper()
	response := dbRegressionCall(t, app, http.MethodPost, "/api/pages",
		`{"title":"Keep page","workspace":true,"content":"Original BlockNote draft"}`, app.handlePages)
	if response.Code != http.StatusCreated {
		t.Fatalf("create page: %d %s", response.Code, response.Body.String())
	}
	var page Page
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil { t.Fatal(err) }
	return page
}

func linkedViewTestCount(t *testing.T, app *AppContext, databaseID string) int {
	t.Helper()
	var count int
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_views WHERE database_id=?", databaseID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestLinkedViewBindingRejectsSecondLinkWithoutOverwritingPage(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, _ := dbRegressionCreate(t, app)
	other, _ := dbRegressionCreate(t, app)
	page := linkedViewTestPage(t, app)
	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providersBefore, err := os.ReadFile(providerPath)
	if err != nil { t.Fatal(err) }

	first := dbRegressionCall(t, app, http.MethodPost, "/api/db/linked-view",
		`{"databaseId":"`+source.ID+`","pageId":"`+page.ID+`","name":"First linked view"}`, app.handleDBLinkedView)
	if first.Code != http.StatusCreated {
		t.Fatalf("first binding: %d %s", first.Code, first.Body.String())
	}
	var view DBView
	if err := json.Unmarshal(first.Body.Bytes(), &view); err != nil { t.Fatal(err) }
	if view.DatabaseID != source.ID { t.Fatalf("linked view owner changed: %+v", view) }

	var linkedDB, linkedView, title, content string
	var version int
	readPage := func() {
		t.Helper()
		if err := app.DB.QueryRow("SELECT linked_database_id,linked_view_id,title,content,version FROM pages WHERE id=?", page.ID).
			Scan(&linkedDB, &linkedView, &title, &content, &version); err != nil { t.Fatal(err) }
	}
	readPage()
	if linkedDB != source.ID || linkedView != view.ID || title != page.Title ||
		content != page.Content || version != page.Version+1 {
		t.Fatalf("first binding corrupted page: db=%q view=%q title=%q content=%q version=%d",
			linkedDB, linkedView, title, content, version)
	}
	sourceCount := linkedViewTestCount(t, app, source.ID)
	otherCount := linkedViewTestCount(t, app, other.ID)
	second := dbRegressionCall(t, app, http.MethodPost, "/api/db/linked-view",
		`{"databaseId":"`+other.ID+`","pageId":"`+page.ID+`","name":"Should not replace"}`, app.handleDBLinkedView)
	if second.Code != http.StatusConflict {
		t.Fatalf("second binding must conflict: %d %s", second.Code, second.Body.String())
	}
	readPage()
	if linkedDB != source.ID || linkedView != view.ID || title != page.Title ||
		content != page.Content || version != page.Version+1 {
		t.Fatalf("rejected rebinding changed page: db=%q view=%q title=%q content=%q version=%d",
			linkedDB, linkedView, title, content, version)
	}
	if linkedViewTestCount(t, app, source.ID) != sourceCount ||
		linkedViewTestCount(t, app, other.ID) != otherCount {
		t.Fatal("rejected rebinding created an orphan view")
	}
	providersAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providersBefore, providersAfter) {
		t.Fatalf("AI provider config changed: %v", err)
	}
}

func TestLinkedViewBindingRejectsBackingPagesIncludingTrashedOwners(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, sourceRecord := dbRegressionCreate(t, app)
	trashedDB, _ := dbRegressionCreate(t, app)
	_, trashedRecord := dbRegressionCreate(t, app)
	freePage := linkedViewTestPage(t, app)

	for _, deletion := range []struct{ path string; handler http.HandlerFunc }{
		{"/api/databases?id=" + trashedDB.ID, app.handleDatabases},
		{"/api/db/records?id=" + trashedRecord.ID, app.handleDBRecords},
	} {
		result := dbRegressionCall(t, app, http.MethodDelete, deletion.path, "", deletion.handler)
		if result.Code != http.StatusNoContent {
			t.Fatalf("trash owner: %d %s", result.Code, result.Body.String())
		}
	}
	before := linkedViewTestCount(t, app, source.ID)
	for _, tc := range []struct {
		name, pageID string
		status int
	}{
		{"active database backing page", source.PageID, http.StatusConflict},
		{"active record backing page", sourceRecord.PageID, http.StatusConflict},
		{"trashed database backing page", trashedDB.PageID, http.StatusConflict},
		{"trashed record backing page", trashedRecord.PageID, http.StatusConflict},
		{"missing page", "page-does-not-exist", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := dbRegressionCall(t, app, http.MethodPost, "/api/db/linked-view",
				`{"databaseId":"`+source.ID+`","pageId":"`+tc.pageID+`"}`, app.handleDBLinkedView)
			if response.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, response.Code, response.Body.String())
			}
			if count := linkedViewTestCount(t, app, source.ID); count != before {
				t.Fatalf("rejected binding inserted view: %d -> %d", before, count)
			}
		})
	}
	trashedSource := dbRegressionCall(t, app, http.MethodPost, "/api/db/linked-view",
		`{"databaseId":"`+trashedDB.ID+`","pageId":"`+freePage.ID+`"}`, app.handleDBLinkedView)
	if trashedSource.Code != http.StatusNotFound {
		t.Fatalf("trashed source accepted: %d %s", trashedSource.Code, trashedSource.Body.String())
	}
	var linkedDB, linkedView sql.NullString
	if err := app.DB.QueryRow("SELECT linked_database_id,linked_view_id FROM pages WHERE id=?", freePage.ID).
		Scan(&linkedDB, &linkedView); err != nil { t.Fatal(err) }
	if linkedDB.Valid || linkedView.Valid {
		t.Fatalf("failed binding mutated free page: %v %v", linkedDB, linkedView)
	}
}

func TestLinkedViewBindingSQLiteFailureRollsBackInsertedView(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, _ := dbRegressionCreate(t, app)
	page := linkedViewTestPage(t, app)
	before := linkedViewTestCount(t, app, source.ID)
	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providersBefore, err := os.ReadFile(providerPath)
	if err != nil { t.Fatal(err) }
	if _, err := app.DB.Exec(`CREATE TRIGGER block_linked_view_binding BEFORE UPDATE OF linked_view_id ON pages
		BEGIN SELECT RAISE(ABORT, 'simulated linked view binding failure'); END`); err != nil { t.Fatal(err) }
	payload := `{"databaseId":"`+source.ID+`","pageId":"`+page.ID+`"}`
	failed := dbRegressionCall(t, app, http.MethodPost, "/api/db/linked-view", payload, app.handleDBLinkedView)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed page binding should be 500: %d %s", failed.Code, failed.Body.String())
	}
	if count := linkedViewTestCount(t, app, source.ID); count != before {
		t.Fatalf("failed page update left orphan view: %d -> %d", before, count)
	}
	var linkedDB, linkedView sql.NullString
	var title, content string
	var version int
	if err := app.DB.QueryRow("SELECT linked_database_id,linked_view_id,title,content,version FROM pages WHERE id=?", page.ID).
		Scan(&linkedDB, &linkedView, &title, &content, &version); err != nil { t.Fatal(err) }
	if linkedDB.Valid || linkedView.Valid || title != page.Title || content != page.Content || version != page.Version {
		t.Fatalf("failed binding modified page: db=%v view=%v title=%q content=%q version=%d",
			linkedDB, linkedView, title, content, version)
	}
	providersAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providersBefore, providersAfter) {
		t.Fatalf("provider config changed: %v", err)
	}
	if _, err := app.DB.Exec("DROP TRIGGER block_linked_view_binding"); err != nil { t.Fatal(err) }
	recovered := dbRegressionCall(t, app, http.MethodPost, "/api/db/linked-view", payload, app.handleDBLinkedView)
	if recovered.Code != http.StatusCreated {
		t.Fatalf("binding after removing fault: %d %s", recovered.Code, recovered.Body.String())
	}
	if count := linkedViewTestCount(t, app, source.ID); count != before+1 {
		t.Fatalf("successful binding not persisted: %d -> %d", before, count)
	}
}
