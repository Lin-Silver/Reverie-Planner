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

func TestPageMoveRejectsInvalidJSONWithoutChangingHierarchy(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	create := func(title, content string) Page {
		t.Helper()
		body, err := json.Marshal(Page{Title: title, Content: content, Workspace: true})
		if err != nil {
			t.Fatal(err)
		}
		res := httptest.NewRecorder()
		app.handlePages(res, httptest.NewRequest(http.MethodPost, "/api/pages", bytes.NewReader(body)))
		if res.Code != http.StatusCreated {
			t.Fatalf("create page: %d %s", res.Code, res.Body.String())
		}
		var page Page
		if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.ID == "" || page.Version != 1 {
			t.Fatalf("unexpected created page: %+v", page)
		}
		return page
	}
	parent := create("Parent", `[{"type":"paragraph","content":[]}]`)
	child := create("Child", `[{"type":"paragraph","content":[{"type":"text","text":"Keep me"}]}]`)

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}

	request := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		app.handlePageMove(res, httptest.NewRequest(http.MethodPost, "/api/pages/move", strings.NewReader(body)))
		return res
	}
	valid := fmt.Sprintf(`{"id":%q,"parentId":%q,"version":%d}`, child.ID, parent.ID, child.Version)
	invalid := []struct{ name, body string }{
		{"trailing object", valid + `{"id":"other"}`},
		{"duplicate parent", fmt.Sprintf(`{"id":%q,"parentId":%q,"parentId":"","version":1}`, child.ID, parent.ID)},
		{"malformed", strings.TrimSuffix(valid, "}")},
		{"null", `null`},
		{"array", `[` + valid + `]`},
		{"oversized", fmt.Sprintf(`{"id":%q,"parentId":%q,"version":1,"padding":%q}`, child.ID, parent.ID, strings.Repeat("x", 4<<20))},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
			}
			got, err := fetchPage(app, child.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.ParentID != "" || got.Title != child.Title || got.Content != child.Content || got.Version != child.Version {
				t.Fatalf("invalid move changed child page: %+v", got)
			}
			gotParent, err := fetchPage(app, parent.ID)
			if err != nil {
				t.Fatal(err)
			}
			if gotParent.ParentID != "" || gotParent.Content != parent.Content || gotParent.Version != parent.Version {
				t.Fatalf("invalid move changed parent page: %+v", gotParent)
			}
			providerAfter, err := os.ReadFile(providerPath)
			if err != nil || !bytes.Equal(providerBefore, providerAfter) {
				t.Fatalf("invalid move changed provider config: %v", err)
			}
		})
	}

	moved := request(valid)
	if moved.Code != http.StatusOK {
		t.Fatalf("valid move: %d %s", moved.Code, moved.Body.String())
	}
	got, err := fetchPage(app, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID != parent.ID || got.Version != child.Version+1 || got.Content != child.Content {
		t.Fatalf("valid move did not preserve content/version: %+v", got)
	}
	stale := request(valid)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale move should conflict, got %d: %s", stale.Code, stale.Body.String())
	}
	got, err = fetchPage(app, child.ID)
	if err != nil || got.ParentID != parent.ID || got.Version != child.Version+1 {
		t.Fatalf("stale move changed hierarchy: %+v err=%v", got, err)
	}
}
