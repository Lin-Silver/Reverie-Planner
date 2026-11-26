package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTemplatesCRUDAndPersistence verifies templates are created, listed,
// updated in place, deleted, and survive a database reopen.
func TestTemplatesCRUDAndPersistence(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	call := func(method, path, body string, wantStatus int) []byte {
		res := httptest.NewRecorder()
		app.handleTemplates(res, httptest.NewRequest(method, path, strings.NewReader(body)))
		if res.Code != wantStatus {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, res.Code, wantStatus, res.Body.String())
		}
		return res.Body.Bytes()
	}

	// Create.
	var created Template
	if err := json.Unmarshal(call(http.MethodPost, "/api/templates", `{"name":"Meeting","title":"Agenda","icon":"📋","content":"# Agenda"}`, http.StatusCreated), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("expected generated template id")
	}
	// Name is required.
	call(http.MethodPost, "/api/templates", `{"title":"x"}`, http.StatusBadRequest)

	// List.
	var listed struct {
		Templates []Template `json:"templates"`
	}
	if err := json.Unmarshal(call(http.MethodGet, "/api/templates", "", http.StatusOK), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Templates) != 1 || listed.Templates[0].Name != "Meeting" {
		t.Fatalf("list: %+v", listed.Templates)
	}

	// Update in place (same id replaces, no duplicate).
	call(http.MethodPost, "/api/templates", `{"id":"`+created.ID+`","name":"Meeting v2","title":"Agenda","content":"# Agenda 2"}`, http.StatusCreated)
	if err := json.Unmarshal(call(http.MethodGet, "/api/templates", "", http.StatusOK), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Templates) != 1 || listed.Templates[0].Name != "Meeting v2" {
		t.Fatalf("update: %+v", listed.Templates)
	}

	// Persists across reopen.
	if err := app.DB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := initializeDatabase(app.RootPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	app.DB = reopened
	if err := json.Unmarshal(call(http.MethodGet, "/api/templates", "", http.StatusOK), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Templates) != 1 {
		t.Fatalf("after reopen: %+v", listed.Templates)
	}

	// Delete.
	if err := json.Unmarshal(call(http.MethodDelete, "/api/templates?id="+created.ID, "", http.StatusOK), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Templates) != 0 {
		t.Fatalf("delete: %+v", listed.Templates)
	}
}