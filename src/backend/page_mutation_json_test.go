package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Invalid page writes must never replace a BlockNote document or advance its
// optimistic version. The same parser guards both page creation and editing.
func TestPageWritesRejectInvalidJSONWithoutLosingContent(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		app.handlePages(res, httptest.NewRequest(method, "/api/pages", strings.NewReader(body)))
		return res
	}

	const originalContent = `[{"type":"paragraph","content":[{"type":"text","text":"Keep this draft"}]}]`
	createBody, err := json.Marshal(Page{Title: "Keep title", Content: originalContent, Workspace: true})
	if err != nil {
		t.Fatal(err)
	}
	created := request(http.MethodPost, string(createBody))
	if created.Code != http.StatusCreated {
		t.Fatalf("create page: status=%d body=%s", created.Code, created.Body.String())
	}
	var page Page
	if err := json.Unmarshal(created.Body.Bytes(), &page); err != nil || page.ID == "" || page.Version != 1 {
		t.Fatalf("invalid page fixture: page=%+v err=%v", page, err)
	}

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, method, body string
	}{
		{"create trailing object", http.MethodPost, `{"title":"Unexpected"}{"title":"Another"}`},
		{"create malformed", http.MethodPost, `{"title":"Unexpected"`},
		{"create null", http.MethodPost, `null`},
		{"create array", http.MethodPost, `[{"title":"Unexpected"}]`},
		{"update trailing object", http.MethodPut, fmt.Sprintf(`{"id":%q,"version":1,"title":"Lost","content":"Lost"}{"id":"other"}`, page.ID)},
		{"update malformed", http.MethodPut, fmt.Sprintf(`{"id":%q,"version":1,"title":"Lost"`, page.ID)},
		{"update oversized", http.MethodPut, fmt.Sprintf(`{"id":%q,"version":1,"content":%q}`, page.ID, strings.Repeat("x", 4<<20))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.method, tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
			}
			var count int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM pages").Scan(&count); err != nil || count != 1 {
				t.Fatalf("page count changed: count=%d err=%v", count, err)
			}
			var title, content string
			var version int
			err := app.DB.QueryRow("SELECT title, content, version FROM pages WHERE id=?", page.ID).Scan(&title, &content, &version)
			if err != nil || title != "Keep title" || content != originalContent || version != 1 {
				t.Fatalf("page changed after invalid write: title=%q content=%q version=%d err=%v", title, content, version, err)
			}
		})
	}
	providerAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, providerAfter) {
		t.Fatalf("page mutation altered provider config: %v", err)
	}

	// The strict parsing guard must not break legitimate optimistic page edits.
	editBody, err := json.Marshal(Page{ID: page.ID, Version: page.Version, Title: "Saved title", Content: originalContent, Workspace: true})
	if err != nil {
		t.Fatal(err)
	}
	updated := request(http.MethodPut, string(editBody))
	if updated.Code != http.StatusOK {
		t.Fatalf("valid page update failed: status=%d body=%s", updated.Code, updated.Body.String())
	}
	var saved Page
	if err := json.Unmarshal(updated.Body.Bytes(), &saved); err != nil || saved.Version != 2 || saved.Content != originalContent {
		t.Fatalf("valid page edit did not persist: page=%+v err=%v", saved, err)
	}
}
