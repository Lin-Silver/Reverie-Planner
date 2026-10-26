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
	"time"
)

func TestSearchAnalyticsAndMembersFailClosedOnSQLiteErrors(t *testing.T) {
	cases := []struct {
		name, table, endpoint string
		handler string
	}{
		{"search projects", "projects", "/api/search?q=missing", "search"},
		{"search tasks", "tasks", "/api/search?q=missing", "search"},
		{"search pages", "pages", "/api/search?q=missing", "search"},
		{"analytics grouped tasks", "tasks", "/api/analytics", "analytics"},
		{"analytics project total", "projects", "/api/analytics", "analytics"},
		{"analytics page total", "pages", "/api/analytics", "analytics"},
		{"analytics cycle total", "cycles", "/api/analytics", "analytics"},
		{"analytics module total", "modules", "/api/analytics", "analytics"},
		{"members query", "members", "/api/members", "members"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupTestContext(t)
			defer app.DB.Close()
			providerPath := filepath.Join(app.RootPath, "config", "providers.json")
			before, err := os.ReadFile(providerPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.DB.Exec(`INSERT INTO app_state (key, value) VALUES ('read-regression', 'preserve-me')`); err != nil {
				t.Fatal(err)
			}
			if _, err := app.DB.Exec("DROP TABLE " + tc.table); err != nil {
				t.Fatalf("failed to construct disposable SQLite fault: %v", err)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tc.endpoint, nil)
			switch tc.handler {
			case "search":
				app.handleSearch(response, request)
			case "analytics":
				app.handleAnalytics(response, request)
			case "members":
				app.handleMembers(response, request)
			}
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("expected HTTP 500 for missing %s, got %d: %s", tc.table, response.Code, response.Body.String())
			}
			var payload map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload["error"] == "" {
				t.Fatalf("missing structured error response: %s err=%v", response.Body.String(), err)
			}
			var marker string
			if err := app.DB.QueryRow(`SELECT value FROM app_state WHERE key='read-regression'`).Scan(&marker); err != nil || marker != "preserve-me" {
				t.Fatalf("failed read changed unrelated app state: value=%q err=%v", marker, err)
			}
			after, err := os.ReadFile(providerPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed read changed AI Provider configuration: %v", err)
			}
		})
	}
}

func TestSearchAnalyticsAndMembersReturnPersistedResults(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	before, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := app.DB.Exec(`INSERT INTO projects (id, name, description, status, created_at) VALUES (?,?,?,?,?)`,
		"read-project", "Read regression", "Find this project", "active", now); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO tasks (id, project_id, title, status, priority, created_at) VALUES (?,?,?,?,?,?)`,
		"read-task", "read-project", "Read regression task", "todo", "high", now); err != nil {
		t.Fatal(err)
	}
	call := func(method, endpoint string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		handler(res, httptest.NewRequest(method, endpoint, nil))
		return res
	}
	search := call(http.MethodGet, "/api/search?q=Read%20regression", app.handleSearch)
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), "read-project") ||
		!strings.Contains(search.Body.String(), "read-task") {
		t.Fatalf("search failed to return persisted records: %d %s", search.Code, search.Body.String())
	}
	analytics := call(http.MethodGet, "/api/analytics?projectId=read-project", app.handleAnalytics)
	if analytics.Code != http.StatusOK {
		t.Fatalf("analytics GET: %d %s", analytics.Code, analytics.Body.String())
	}
	var totals struct {
		TaskStatusCounts map[string]int `json:"taskStatusCounts"`
		TotalProjects int `json:"totalProjects"`
		TotalTasks int `json:"totalTasks"`
	}
	if err := json.Unmarshal(analytics.Body.Bytes(), &totals); err != nil ||
		totals.TaskStatusCounts["todo"] != 1 || totals.TotalProjects < 1 || totals.TotalTasks < 1 {
		t.Fatalf("unexpected analytics totals: %+v err=%v", totals, err)
	}
	first := call(http.MethodGet, "/api/members", app.handleMembers)
	second := call(http.MethodGet, "/api/members", app.handleMembers)
	for i, response := range []*httptest.ResponseRecorder{first, second} {
		var members []Member
		if err := json.Unmarshal(response.Body.Bytes(), &members); err != nil ||
			response.Code != http.StatusOK || len(members) != 1 ||
			members[0].ID != "mem-you" || members[0].Role != "owner" {
			t.Fatalf("members GET %d: status=%d members=%+v err=%v", i+1, response.Code, members, err)
		}
	}
	var count int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM members WHERE id='mem-you'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("default member was not persisted exactly once: count=%d err=%v", count, err)
	}
	for _, endpoint := range []struct {
		path string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"/api/search?q=test", app.handleSearch},
		{"/api/analytics", app.handleAnalytics},
		{"/api/members", app.handleMembers},
	} {
		response := call(http.MethodPost, endpoint.path, endpoint.handler)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("read-only endpoint accepted POST %s: %d", endpoint.path, response.Code)
		}
	}
	after, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("read endpoints changed AI Provider configuration: %v", err)
	}
}

func TestDefaultMemberInitializationDoesNotReturnPhantomSuccess(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	if _, err := app.DB.Exec(`CREATE TRIGGER deny_member_insert BEFORE INSERT ON members
		BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		app.handleMembers(res, httptest.NewRequest(http.MethodGet, "/api/members", nil))
		return res
	}
	failed := request()
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed default member INSERT returned %d: %s", failed.Code, failed.Body.String())
	}
	var count int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM members`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed initialization created a member: count=%d err=%v", count, err)
	}
	if _, err := app.DB.Exec(`DROP TRIGGER deny_member_insert`); err != nil {
		t.Fatal(err)
	}
	recovered := request()
	if recovered.Code != http.StatusOK {
		t.Fatalf("default member failed after removing test-only trigger: %d %s", recovered.Code, recovered.Body.String())
	}
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM members WHERE id='mem-you'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recovered default member not persisted: count=%d err=%v", count, err)
	}
}
