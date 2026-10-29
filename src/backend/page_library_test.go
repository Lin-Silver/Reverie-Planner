package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageLibraryPortableAndIndependent(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	if _, err := a.DB.Exec(`INSERT INTO pages(id,title,workspace) VALUES ('one','A page',1)`); err != nil {
		t.Fatal(err)
	}
	post := func(body string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		a.handlePageLibrary(w, httptest.NewRequest("POST", "/api/page-library", strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	}
	get := func() map[string][]string {
		t.Helper()
		w := httptest.NewRecorder()
		a.handlePageLibrary(w, httptest.NewRequest("GET", "/api/page-library", nil))
		var out map[string][]string
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	post(`{"id":"one","favorite":true}`, 200)
	post(`{"id":"one","visit":true}`, 200)
	// Simulate closing and reopening a portable workspace, not browser storage.
	a.DB.Close()
	var err error
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	state := get()
	if len(state["favorites"]) != 1 || len(state["recent"]) != 1 {
		t.Fatalf("metadata lost: %v", state)
	}
	post(`{"id":"one","favorite":false}`, 200)
	state = get()
	if len(state["favorites"]) != 0 || len(state["recent"]) != 1 {
		t.Fatalf("toggle overwrote visit: %v", state)
	}
	for _, body := range []string{`{}`, `{"id":"one"}`, `{"id":"one","favorite":null}`, `{"id":"one","favorite":true,"visit":true}`, `{"id":"one","favorite":"yes"}`} {
		post(body, 400)
	}
	post(`{"id":"missing","favorite":true}`, 404)
	if _, err := a.DB.Exec(`DELETE FROM pages WHERE id='one'`); err != nil {
		t.Fatal(err)
	}
	state = get()
	if len(state["favorites"]) != 0 || len(state["recent"]) != 0 {
		t.Fatalf("deleted page leaked: %v", state)
	}
}
