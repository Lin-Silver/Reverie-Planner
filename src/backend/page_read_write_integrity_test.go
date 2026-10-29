package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A failed page INSERT/DELETE must not report success or change an existing
// BlockNote document. Read failures must not masquerade as empty/not-found data.
func TestPageReadCreateDeleteFailuresPreserveExistingData(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providersBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}

	request := func(method, path string, page *Page) *httptest.ResponseRecorder {
		t.Helper()
		var body *bytes.Reader
		if page != nil {
			payload, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(payload)
		} else {
			body = bytes.NewReader(nil)
		}
		res := httptest.NewRecorder()
		app.handlePages(res, httptest.NewRequest(method, path, body))
		return res
	}

	const originalID = "page-read-write-regression"
	const originalContent = "[{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"Keep this document\"}]}]"
	original := Page{ID: originalID, Title: "Keep title", Content: originalContent, Workspace: true}
	created := request(http.MethodPost, "/api/pages", &original)
	if created.Code != http.StatusCreated {
		t.Fatalf("create fixture: %d %s", created.Code, created.Body.String())
	}
	assertPreserved := func() {
		t.Helper()
		var count int
		if err := app.DB.QueryRow("SELECT COUNT(*) FROM pages").Scan(&count); err != nil || count != 1 {
			t.Fatalf("page count changed: count=%d err=%v", count, err)
		}
		var title, content string
		var version int
		if err := app.DB.QueryRow("SELECT title, content, version FROM pages WHERE id=?", originalID).
			Scan(&title, &content, &version); err != nil || title != original.Title || content != originalContent || version != 1 {
			t.Fatalf("existing page changed: title=%q content=%q version=%d err=%v", title, content, version, err)
		}
		after, err := os.ReadFile(providerPath)
		if err != nil || !bytes.Equal(providersBefore, after) {
			t.Fatalf("AI Provider configuration changed: %v", err)
		}
	}
	assertError := func(res *httptest.ResponseRecorder, want int) {
		t.Helper()
		if res.Code != want {
			t.Fatalf("expected HTTP %d, got %d: %s", want, res.Code, res.Body.String())
		}
		var payload map[string]string
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil || payload["error"] == "" {
			t.Fatalf("expected structured error response: %s err=%v", res.Body.String(), err)
		}
	}

	assertError(request(http.MethodPost, "/api/pages", &Page{
		ID: originalID, Title: "Overwrite attempt", Content: "Lost", Workspace: true,
	}), http.StatusInternalServerError)
	assertPreserved()

	if _, err := app.DB.Exec("CREATE TRIGGER reject_page_insert BEFORE INSERT ON pages BEGIN SELECT RAISE(ABORT, 'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	assertError(request(http.MethodPost, "/api/pages", &Page{
		ID: "blocked-new-page", Title: "Phantom page", Content: "Lost", Workspace: true,
	}), http.StatusInternalServerError)
	assertPreserved()
	if _, err := app.DB.Exec("DROP TRIGGER reject_page_insert"); err != nil {
		t.Fatal(err)
	}

	assertError(request(http.MethodGet, "/api/pages?id=missing-page", nil), http.StatusNotFound)
	single := request(http.MethodGet, "/api/pages?id="+originalID, nil)
	if single.Code != http.StatusOK {
		t.Fatalf("valid page GET: %d %s", single.Code, single.Body.String())
	}
	var loaded Page
	if err := json.Unmarshal(single.Body.Bytes(), &loaded); err != nil ||
		loaded.ID != originalID || loaded.Title != original.Title ||
		loaded.Content != originalContent || loaded.Version != 1 {
		t.Fatalf("valid page GET lost persisted fields: %+v err=%v", loaded, err)
	}
	list := request(http.MethodGet, "/api/pages", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("valid page list: %d %s", list.Code, list.Body.String())
	}
	var pages []Page
	if err := json.Unmarshal(list.Body.Bytes(), &pages); err != nil ||
		len(pages) != 1 || pages[0].ID != originalID || pages[0].Version != 1 {
		t.Fatalf("valid page list omitted persisted page: %+v err=%v", pages, err)
	}

	// SQLite permits text in non-STRICT INTEGER columns. A failed Scan must
	// return HTTP 500 rather than a false 404 or an incomplete 200 response.
	if _, err := app.DB.Exec("UPDATE pages SET version='invalid-version' WHERE id=?", originalID); err != nil {
		t.Fatal(err)
	}
	assertError(request(http.MethodGet, "/api/pages?id="+originalID, nil), http.StatusInternalServerError)
	assertError(request(http.MethodGet, "/api/pages", nil), http.StatusInternalServerError)
	if _, err := app.DB.Exec("UPDATE pages SET version=1 WHERE id=?", originalID); err != nil {
		t.Fatal(err)
	}
	assertPreserved()

	assertError(request(http.MethodDelete, "/api/pages", nil), http.StatusBadRequest)
	assertError(request(http.MethodDelete, "/api/pages?id=missing-page", nil), http.StatusNotFound)
	assertPreserved()
	if _, err := app.DB.Exec("CREATE TRIGGER reject_page_delete BEFORE DELETE ON pages BEGIN SELECT RAISE(ABORT, 'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	assertError(request(http.MethodDelete, "/api/pages?id="+originalID, nil), http.StatusInternalServerError)
	assertPreserved()
	if _, err := app.DB.Exec("DROP TRIGGER reject_page_delete"); err != nil {
		t.Fatal(err)
	}

	deleted := request(http.MethodDelete, "/api/pages?id="+originalID, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("valid page deletion failed: %d %s", deleted.Code, deleted.Body.String())
	}
	var count int
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM pages WHERE id=?", originalID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("page deletion was not persisted: count=%d err=%v", count, err)
	}
	after, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providersBefore, after) {
		t.Fatalf("valid page deletion changed AI Provider configuration: %v", err)
	}
}

func TestPageReadMissingTableReturnsServerError(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	before, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec("DROP TABLE pages"); err != nil {
		t.Fatalf("could not construct disposable missing-table fixture: %v", err)
	}
	for _, path := range []string{"/api/pages", "/api/pages?id=missing"} {
		t.Run(strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
			res := httptest.NewRecorder()
			app.handlePages(res, httptest.NewRequest(http.MethodGet, path, nil))
			if res.Code != http.StatusInternalServerError {
				t.Fatalf("missing pages table returned %d: %s", res.Code, res.Body.String())
			}
			var payload map[string]string
			if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil || payload["error"] == "" {
				t.Fatalf("missing structured error: %s err=%v", res.Body.String(), err)
			}
		})
	}
	after, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("read failures changed AI Provider configuration: %v", err)
	}
}
