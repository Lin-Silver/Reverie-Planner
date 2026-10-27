package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// A linked view and a database backing page must never share ownership,
// including when the linked view's source database has been soft-deleted.
func TestDatabaseCreateRejectsLinkedViewPagesIncludingTrashedSources(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, _ := dbRegressionCreate(t, app)
	page := linkedViewTestPage(t, app)
	binding := dbRegressionCall(t, app, http.MethodPost, "/api/db/linked-view",
		fmt.Sprintf(`{"databaseId":%q,"pageId":%q}`, source.ID, page.ID), app.handleDBLinkedView)
	if binding.Code != http.StatusCreated {
		t.Fatalf("create linked view: %d %s", binding.Code, binding.Body.String())
	}

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	var linkedDatabase, linkedView, title, content string
	var version int
	if err := app.DB.QueryRow(`SELECT linked_database_id, linked_view_id, title, content, version
		FROM pages WHERE id=?`, page.ID).
		Scan(&linkedDatabase, &linkedView, &title, &content, &version); err != nil {
		t.Fatal(err)
	}
	if linkedDatabase != source.ID || linkedView == "" || version != page.Version+1 {
		t.Fatalf("linked page was not correctly bound: db=%q view=%q version=%d", linkedDatabase, linkedView, version)
	}

	counts := func() [4]int {
		t.Helper()
		var result [4]int
		for i, table := range []string{"pages", "databases", "db_properties", "db_views"} {
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&result[i]); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	assertRejected := func() {
		t.Helper()
		before := counts()
		response := dbRegressionCall(t, app, http.MethodPost, "/api/databases",
			fmt.Sprintf(`{"title":"Must not bind","pageId":%q}`, page.ID), app.handleDatabases)
		if response.Code != http.StatusConflict {
			t.Fatalf("linked page accepted as database backing page: %d %s", response.Code, response.Body.String())
		}
		if after := counts(); after != before {
			t.Fatalf("rejected binding left database or default schema: before=%v after=%v", before, after)
		}
		var gotDatabase, gotView, gotTitle, gotContent string
		var gotVersion int
		if err := app.DB.QueryRow(`SELECT linked_database_id, linked_view_id, title, content, version
			FROM pages WHERE id=?`, page.ID).
			Scan(&gotDatabase, &gotView, &gotTitle, &gotContent, &gotVersion); err != nil {
			t.Fatal(err)
		}
		if gotDatabase != linkedDatabase || gotView != linkedView || gotTitle != title ||
			gotContent != content || gotVersion != version {
			t.Fatalf("rejected binding changed page: db=%q view=%q title=%q content=%q version=%d",
				gotDatabase, gotView, gotTitle, gotContent, gotVersion)
		}
		providerAfter, err := os.ReadFile(providerPath)
		if err != nil || !bytes.Equal(providerBefore, providerAfter) {
			t.Fatalf("provider configuration changed: %v", err)
		}
	}
	assertRejected()
	deleted := dbRegressionCall(t, app, http.MethodDelete,
		"/api/databases?id="+source.ID, "", app.handleDatabases)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("soft-delete source: %d %s", deleted.Code, deleted.Body.String())
	}
	assertRejected()
}

// Even a partially populated legacy link must not be silently overwritten.
func TestDatabaseCreateRejectsPartiallyBoundLinkedPages(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, _ := dbRegressionCreate(t, app)
	page := linkedViewTestPage(t, app)
	for _, tc := range []struct {
		name, sql, value string
	}{
		{"view only", "UPDATE pages SET linked_database_id=NULL, linked_view_id=? WHERE id=?", source.Views[0].ID},
		{"database only", "UPDATE pages SET linked_view_id=NULL, linked_database_id=? WHERE id=?", source.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := app.DB.Exec(tc.sql, tc.value, page.ID); err != nil {
				t.Fatal(err)
			}
			var before int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM databases").Scan(&before); err != nil {
				t.Fatal(err)
			}
			response := dbRegressionCall(t, app, http.MethodPost, "/api/databases",
				fmt.Sprintf(`{"title":"Must not bind","pageId":%q}`, page.ID), app.handleDatabases)
			if response.Code != http.StatusConflict {
				t.Fatalf("partially linked page accepted: %d %s", response.Code, response.Body.String())
			}
			var after int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM databases").Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("rejected binding inserted a database: %d -> %d", before, after)
			}
			var linkedDatabase, linkedView sql.NullString
			if err := app.DB.QueryRow("SELECT linked_database_id, linked_view_id FROM pages WHERE id=?", page.ID).
				Scan(&linkedDatabase, &linkedView); err != nil {
				t.Fatal(err)
			}
			if tc.name == "view only" && (linkedDatabase.Valid || linkedView.String != tc.value) {
				t.Fatalf("rejected binding changed view-only metadata: db=%v view=%v", linkedDatabase, linkedView)
			}
			if tc.name == "database only" && (linkedDatabase.String != tc.value || linkedView.Valid) {
				t.Fatalf("rejected binding changed database-only metadata: db=%v view=%v", linkedDatabase, linkedView)
			}
		})
	}
}
