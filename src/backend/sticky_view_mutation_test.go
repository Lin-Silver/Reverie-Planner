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
	"time"
)

func TestStickyAndSavedViewMutationsPreserveDataOnFailure(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	const projectID = "sticky-view-regression-project"
	if _, err := app.DB.Exec(`INSERT INTO projects (id, name, status, created_at) VALUES (?,?,?,?)`,
		projectID, "Keep project", "active", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	request := func(method, path, body string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response
	}

	stickyCreated := request(http.MethodPost, "/api/stickies",
		`{"title":"Keep sticky","body":"Keep note","color":"blue"}`, app.handleStickies)
	if stickyCreated.Code != http.StatusCreated {
		t.Fatalf("create sticky: %d %s", stickyCreated.Code, stickyCreated.Body.String())
	}
	var sticky Sticky
	if err := json.Unmarshal(stickyCreated.Body.Bytes(), &sticky); err != nil || sticky.ID == "" {
		t.Fatalf("decode sticky: %+v err=%v", sticky, err)
	}
	viewCreated := request(http.MethodPost, "/api/views",
		fmt.Sprintf(`{"projectId":%q,"name":"Keep view","filters":"status:open"}`, projectID), app.handleViews)
	if viewCreated.Code != http.StatusCreated {
		t.Fatalf("create view: %d %s", viewCreated.Code, viewCreated.Body.String())
	}
	var view View
	if err := json.Unmarshal(viewCreated.Body.Bytes(), &view); err != nil || view.ID == "" {
		t.Fatalf("decode view: %+v err=%v", view, err)
	}

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	checkUnchanged := func(t *testing.T) {
		t.Helper()
		var stickyCount, viewCount int
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM stickies`).Scan(&stickyCount); err != nil || stickyCount != 1 {
			t.Fatalf("sticky count=%d err=%v", stickyCount, err)
		}
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM views`).Scan(&viewCount); err != nil || viewCount != 1 {
			t.Fatalf("view count=%d err=%v", viewCount, err)
		}
		var title, body, color string
		if err := app.DB.QueryRow(`SELECT title, body, color FROM stickies WHERE id=?`, sticky.ID).
			Scan(&title, &body, &color); err != nil || title != "Keep sticky" || body != "Keep note" || color != "blue" {
			t.Fatalf("sticky changed: title=%q body=%q color=%q err=%v", title, body, color, err)
		}
		var project, name, filters string
		if err := app.DB.QueryRow(`SELECT project_id, name, filters FROM views WHERE id=?`, view.ID).
			Scan(&project, &name, &filters); err != nil || project != projectID || name != "Keep view" || filters != "status:open" {
			t.Fatalf("view changed: project=%q name=%q filters=%q err=%v", project, name, filters, err)
		}
		var projectName string
		if err := app.DB.QueryRow(`SELECT name FROM projects WHERE id=?`, projectID).
			Scan(&projectName); err != nil || projectName != "Keep project" {
			t.Fatalf("project changed: name=%q err=%v", projectName, err)
		}
		providerAfter, err := os.ReadFile(providerPath)
		if err != nil || !bytes.Equal(providerBefore, providerAfter) {
			t.Fatalf("provider configuration changed: %v", err)
		}
	}

	cases := []struct {
		name, method, path, body string
		handler func(http.ResponseWriter, *http.Request)
		want int
	}{
		{"sticky malformed", http.MethodPost, "/api/stickies", `{"title":`, app.handleStickies, 400},
		{"sticky duplicate", http.MethodPost, "/api/stickies", `{"title":"one","title":"two"}`, app.handleStickies, 400},
		{"sticky trailing", http.MethodPost, "/api/stickies", `{"title":"one"}{}`, app.handleStickies, 400},
		{"sticky wrong type", http.MethodPost, "/api/stickies", `{"body":12}`, app.handleStickies, 400},
		{"sticky null", http.MethodPost, "/api/stickies", `null`, app.handleStickies, 400},
		{"sticky array", http.MethodPost, "/api/stickies", `[{}]`, app.handleStickies, 400},
		{"sticky oversized", http.MethodPost, "/api/stickies", fmt.Sprintf(`{"body":%q}`, strings.Repeat("x", 4<<20)), app.handleStickies, 400},
		{"sticky duplicate id", http.MethodPost, "/api/stickies", fmt.Sprintf(`{"id":%q,"title":"Overwrite"}`, sticky.ID), app.handleStickies, 500},
		{"sticky delete missing id", http.MethodDelete, "/api/stickies", "", app.handleStickies, 400},
		{"sticky delete unknown id", http.MethodDelete, "/api/stickies?id=missing", "", app.handleStickies, 404},
		{"view malformed", http.MethodPost, "/api/views", `{"name":`, app.handleViews, 400},
		{"view duplicate", http.MethodPost, "/api/views", `{"name":"one","name":"two"}`, app.handleViews, 400},
		{"view trailing", http.MethodPost, "/api/views", `{"name":"one"}{}`, app.handleViews, 400},
		{"view wrong type", http.MethodPost, "/api/views", `{"name":4}`, app.handleViews, 400},
		{"view null", http.MethodPost, "/api/views", `null`, app.handleViews, 400},
		{"view array", http.MethodPost, "/api/views", `[{"name":"one"}]`, app.handleViews, 400},
		{"view oversized", http.MethodPost, "/api/views", fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 4<<20)), app.handleViews, 400},
		{"view blank name", http.MethodPost, "/api/views", `{"name":"   "}`, app.handleViews, 400},
		{"view duplicate id", http.MethodPost, "/api/views", fmt.Sprintf(`{"id":%q,"name":"Overwrite"}`, view.ID), app.handleViews, 500},
		{"view missing project", http.MethodPost, "/api/views", `{"projectId":"missing","name":"Bad"}`, app.handleViews, 500},
		{"view delete missing id", http.MethodDelete, "/api/views", "", app.handleViews, 400},
		{"view delete unknown id", http.MethodDelete, "/api/views?id=missing", "", app.handleViews, 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := request(tc.method, tc.path, tc.body, tc.handler)
			if response.Code != tc.want {
				t.Fatalf("expected %d got %d: %s", tc.want, response.Code, response.Body.String())
			}
			checkUnchanged(t)
		})
	}
	for _, tc := range []struct {
		path string
		handler func(http.ResponseWriter, *http.Request)
		id string
	}{
		{"/api/stickies", app.handleStickies, sticky.ID},
		{"/api/views", app.handleViews, view.ID},
	} {
		response := request(http.MethodGet, tc.path, "", tc.handler)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), tc.id) {
			t.Fatalf("list %s: %d %s", tc.path, response.Code, response.Body.String())
		}
	}
	for _, tc := range []struct {
		path string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"/api/stickies?id=" + sticky.ID, app.handleStickies},
		{"/api/views?id=" + view.ID, app.handleViews},
	} {
		response := request(http.MethodDelete, tc.path, "", tc.handler)
		if response.Code != http.StatusNoContent {
			t.Fatalf("delete %s: %d %s", tc.path, response.Code, response.Body.String())
		}
	}
	for _, table := range []string{"stickies", "views"} {
		var count int
		if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s deletion not persisted: count=%d err=%v", table, count, err)
		}
	}
	providerAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, providerAfter) {
		t.Fatalf("provider configuration changed after valid deletes: %v", err)
	}
}
