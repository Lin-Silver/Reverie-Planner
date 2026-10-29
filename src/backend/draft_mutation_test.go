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

func TestDraftWritesRejectInvalidJSONAndPreserveExistingContent(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		app.handleDrafts(res, httptest.NewRequest(method, "/api/drafts", strings.NewReader(body)))
		return res
	}
	created := request(http.MethodPost, `{"kind":"note","title":"Keep title","content":"Keep content"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create draft: %d %s", created.Code, created.Body.String())
	}
	var original Draft
	if err := json.Unmarshal(created.Body.Bytes(), &original); err != nil || original.ID == "" {
		t.Fatalf("decode created draft: %+v err=%v", original, err)
	}

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	checkUnchanged := func(t *testing.T) {
		t.Helper()
		var count int
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM drafts`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("draft count changed: %d err=%v", count, err)
		}
		var kind, title, content, updated string
		if err := app.DB.QueryRow(`SELECT kind, title, content, updated_at FROM drafts WHERE id=?`, original.ID).Scan(&kind, &title, &content, &updated); err != nil {
			t.Fatal(err)
		}
		if kind != original.Kind || title != original.Title || content != original.Content || updated != original.UpdatedAt.Format("2006-01-02T15:04:05Z07:00") {
			t.Fatalf("rejected request changed draft: kind=%q title=%q content=%q updated=%q", kind, title, content, updated)
		}
		providerAfter, err := os.ReadFile(providerPath)
		if err != nil || !bytes.Equal(providerBefore, providerAfter) {
			t.Fatalf("provider configuration changed: %v", err)
		}
	}
	invalid := []struct {
		name, method, body string
	}{
		{"trailing post", http.MethodPost, `{"title":"Bad"}{"title":"Other"}`},
		{"duplicate post", http.MethodPost, `{"title":"Bad","title":"Other"}`},
		{"null post", http.MethodPost, `null`},
		{"array post", http.MethodPost, `[{"title":"Bad"}]`},
		{"oversized post", http.MethodPost, fmt.Sprintf(`{"title":%q}`, strings.Repeat("x", 4<<20))},
		{"trailing put", http.MethodPut, fmt.Sprintf(`{"id":%q,"content":"Bad"}{"content":"Other"}`, original.ID)},
		{"duplicate put", http.MethodPut, fmt.Sprintf(`{"id":%q,"content":"Bad","content":"Other"}`, original.ID)},
		{"malformed put", http.MethodPut, fmt.Sprintf(`{"id":%q,"content":"Bad"`, original.ID)},
		{"null put", http.MethodPut, `null`},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.method, tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
			}
			checkUnchanged(t)
		})
	}

	partial := request(http.MethodPut, fmt.Sprintf(`{"id":%q,"content":"New content"}`, original.ID))
	if partial.Code != http.StatusOK {
		t.Fatalf("partial update: %d %s", partial.Code, partial.Body.String())
	}
	var updated Draft
	if err := json.Unmarshal(partial.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.Kind != original.Kind || updated.Title != original.Title || updated.Content != "New content" {
		t.Fatalf("partial update lost existing fields: %+v", updated)
	}
	if err := app.DB.QueryRow(`SELECT kind, title, content FROM drafts WHERE id=?`, original.ID).Scan(&updated.Kind, &updated.Title, &updated.Content); err != nil || updated.Kind != original.Kind || updated.Title != original.Title || updated.Content != "New content" {
		t.Fatalf("partial update was not persisted correctly: %+v err=%v", updated, err)
	}
	clear := request(http.MethodPut, fmt.Sprintf(`{"id":%q,"title":""}`, original.ID))
	if clear.Code != http.StatusOK {
		t.Fatalf("explicit clear: %d %s", clear.Code, clear.Body.String())
	}
	if err := app.DB.QueryRow(`SELECT title, content FROM drafts WHERE id=?`, original.ID).Scan(&updated.Title, &updated.Content); err != nil || updated.Title != "" || updated.Content != "New content" {
		t.Fatalf("explicit clear did not preserve content: %+v err=%v", updated, err)
	}
	missing := request(http.MethodPut, `{"id":"missing-draft","content":"No"}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing draft must return 404, got %d: %s", missing.Code, missing.Body.String())
	}
	providerAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, providerAfter) {
		t.Fatalf("valid draft updates changed provider configuration: %v", err)
	}
}
