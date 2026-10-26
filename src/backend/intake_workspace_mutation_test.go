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

func TestIntakeAndWorkspaceMutationsPreserveDataOnFailure(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	const projectID = "intake-workspace-project"
	if _, err := app.DB.Exec(`INSERT INTO projects (id, name, status, created_at) VALUES (?,?,?,?)`,
		projectID, "Keep project", "active", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if strings.HasPrefix(path, "/api/intake") {
			app.handleIntake(response, req)
		} else {
			app.handleWorkspaces(response, req)
		}
		return response
	}

	// The legacy GET endpoint initializes a default workspace only when none exists.
	defaultResponse := request(http.MethodGet, "/api/workspaces", "")
	if defaultResponse.Code != http.StatusOK {
		t.Fatalf("default workspace GET: %d %s", defaultResponse.Code, defaultResponse.Body.String())
	}
	var defaults []Workspace
	if err := json.Unmarshal(defaultResponse.Body.Bytes(), &defaults); err != nil || len(defaults) != 1 ||
		defaults[0].ID != "ws-default" || defaults[0].Name != "Workspace" {
		t.Fatalf("default workspace not persisted: %+v err=%v", defaults, err)
	}
	workspaceResponse := request(http.MethodPost, "/api/workspaces",
		`{"id":"ws-regression","name":"Keep workspace","slug":"keep","description":"Keep description"}`)
	if workspaceResponse.Code != http.StatusCreated {
		t.Fatalf("workspace POST: %d %s", workspaceResponse.Code, workspaceResponse.Body.String())
	}
	intakeResponse := request(http.MethodPost, "/api/intake",
		fmt.Sprintf(`{"id":"intake-regression","projectId":%q,"title":"Keep intake","body":"Keep body"}`, projectID))
	if intakeResponse.Code != http.StatusCreated {
		t.Fatalf("intake POST: %d %s", intakeResponse.Code, intakeResponse.Body.String())
	}
	var created IntakeItem
	if err := json.Unmarshal(intakeResponse.Body.Bytes(), &created); err != nil ||
		created.ID != "intake-regression" || created.Status != "pending" {
		t.Fatalf("intake default status: %+v err=%v", created, err)
	}

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providersBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	assertUnchanged := func(t *testing.T, expectedStatus string) {
		t.Helper()
		var count int
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM intake`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("intake count=%d err=%v", count, err)
		}
		var project, title, body, status string
		if err := app.DB.QueryRow(`SELECT project_id, title, body, status FROM intake WHERE id=?`, created.ID).
			Scan(&project, &title, &body, &status); err != nil ||
			project != projectID || title != "Keep intake" || body != "Keep body" || status != expectedStatus {
			t.Fatalf("intake changed: project=%q title=%q body=%q status=%q err=%v", project, title, body, status, err)
		}
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM workspaces`).Scan(&count); err != nil || count != 2 {
			t.Fatalf("workspace count=%d err=%v", count, err)
		}
		var name, slug, description string
		if err := app.DB.QueryRow(`SELECT name, slug, description FROM workspaces WHERE id=?`, "ws-regression").
			Scan(&name, &slug, &description); err != nil ||
			name != "Keep workspace" || slug != "keep" || description != "Keep description" {
			t.Fatalf("workspace changed: name=%q slug=%q description=%q err=%v", name, slug, description, err)
		}
		if err := app.DB.QueryRow(`SELECT name FROM projects WHERE id=?`, projectID).Scan(&name); err != nil || name != "Keep project" {
			t.Fatalf("project changed: name=%q err=%v", name, err)
		}
		providersAfter, err := os.ReadFile(providerPath)
		if err != nil || !bytes.Equal(providersBefore, providersAfter) {
			t.Fatalf("AI Provider configuration changed: %v", err)
		}
	}

	invalid := []struct {
		name, method, path, body string
		status int
	}{
		{"intake malformed", http.MethodPost, "/api/intake", `{"title":`, 400},
		{"intake duplicate", http.MethodPost, "/api/intake", `{"title":"one","title":"two"}`, 400},
		{"intake trailing", http.MethodPost, "/api/intake", `{"title":"one"}{}`, 400},
		{"intake wrong type", http.MethodPost, "/api/intake", `{"title":4}`, 400},
		{"intake null", http.MethodPost, "/api/intake", `null`, 400},
		{"intake array", http.MethodPost, "/api/intake", `[{}]`, 400},
		{"intake oversized", http.MethodPost, "/api/intake", fmt.Sprintf(`{"title":%q}`, strings.Repeat("x", 4<<20)), 400},
		{"intake missing project", http.MethodPost, "/api/intake", `{"title":"Bad"}`, 400},
		{"intake blank title", http.MethodPost, "/api/intake", fmt.Sprintf(`{"projectId":%q,"title":"  "}`, projectID), 400},
		{"intake blank status", http.MethodPost, "/api/intake", fmt.Sprintf(`{"projectId":%q,"title":"Bad","status":"  "}`, projectID), 400},
		{"intake missing foreign project", http.MethodPost, "/api/intake", `{"projectId":"missing","title":"Bad"}`, 500},
		{"intake duplicate id", http.MethodPost, "/api/intake", fmt.Sprintf(`{"id":%q,"projectId":%q,"title":"Bad"}`, created.ID, projectID), 500},
		{"intake put malformed", http.MethodPut, "/api/intake", `{"id":`, 400},
		{"intake put duplicate", http.MethodPut, "/api/intake", fmt.Sprintf(`{"id":%q,"status":"one","status":"two"}`, created.ID), 400},
		{"intake put trailing", http.MethodPut, "/api/intake", fmt.Sprintf(`{"id":%q,"status":"done"}{}`, created.ID), 400},
		{"intake put null", http.MethodPut, "/api/intake", `null`, 400},
		{"intake put array", http.MethodPut, "/api/intake", `[{}]`, 400},
		{"intake put oversized", http.MethodPut, "/api/intake", fmt.Sprintf(`{"id":%q,"status":%q}`, created.ID, strings.Repeat("x", 4<<20)), 400},
		{"intake put missing id", http.MethodPut, "/api/intake", `{"status":"done"}`, 400},
		{"intake put blank status", http.MethodPut, "/api/intake", fmt.Sprintf(`{"id":%q,"status":"  "}`, created.ID), 400},
		{"intake put unknown id", http.MethodPut, "/api/intake", `{"id":"missing","status":"done"}`, 404},
		{"intake delete missing id", http.MethodDelete, "/api/intake", "", 400},
		{"intake delete unknown id", http.MethodDelete, "/api/intake?id=missing", "", 404},
		{"workspace malformed", http.MethodPost, "/api/workspaces", `{"name":`, 400},
		{"workspace duplicate", http.MethodPost, "/api/workspaces", `{"name":"one","name":"two"}`, 400},
		{"workspace trailing", http.MethodPost, "/api/workspaces", `{"name":"one"}{}`, 400},
		{"workspace wrong type", http.MethodPost, "/api/workspaces", `{"name":4}`, 400},
		{"workspace null", http.MethodPost, "/api/workspaces", `null`, 400},
		{"workspace array", http.MethodPost, "/api/workspaces", `[{}]`, 400},
		{"workspace oversized", http.MethodPost, "/api/workspaces", fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 4<<20)), 400},
		{"workspace duplicate id", http.MethodPost, "/api/workspaces", `{"id":"ws-regression","name":"Overwrite"}`, 500},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.method, tc.path, tc.body)
			if res.Code != tc.status {
				t.Fatalf("expected %d got %d: %s", tc.status, res.Code, res.Body.String())
			}
			assertUnchanged(t, "pending")
		})
	}

	// Valid status updates must not clear the intake title, body or project.
	updated := request(http.MethodPut, "/api/intake", fmt.Sprintf(`{"id":%q,"status":"approved"}`, created.ID))
	if updated.Code != http.StatusOK {
		t.Fatalf("intake PUT: %d %s", updated.Code, updated.Body.String())
	}
	assertUnchanged(t, "approved")
	intakeList := request(http.MethodGet, "/api/intake?projectId="+projectID, "")
	if intakeList.Code != http.StatusOK || !strings.Contains(intakeList.Body.String(), created.ID) {
		t.Fatalf("intake GET: %d %s", intakeList.Code, intakeList.Body.String())
	}
	workspaceList := request(http.MethodGet, "/api/workspaces", "")
	if workspaceList.Code != http.StatusOK ||
		!strings.Contains(workspaceList.Body.String(), "ws-regression") ||
		!strings.Contains(workspaceList.Body.String(), "ws-default") {
		t.Fatalf("workspace GET: %d %s", workspaceList.Code, workspaceList.Body.String())
	}
	deleted := request(http.MethodDelete, "/api/intake?id="+created.ID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("intake DELETE: %d %s", deleted.Code, deleted.Body.String())
	}
	var count int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM intake`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("intake deletion not persisted: count=%d err=%v", count, err)
	}
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM workspaces`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("intake deletion changed workspaces: count=%d err=%v", count, err)
	}
	providersAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providersBefore, providersAfter) {
		t.Fatalf("AI Provider configuration changed after valid writes: %v", err)
	}
}

func TestIntakeAndWorkspaceDatabaseErrorsAreNotReportedAsSuccess(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	for _, tc := range []struct {
		table, path, body string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"intake", "/api/intake", `{"projectId":"missing","title":"bad"}`, app.handleIntake},
		{"workspaces", "/api/workspaces", `{"name":"bad"}`, app.handleWorkspaces},
	} {
		t.Run(tc.table, func(t *testing.T) {
			if _, err := app.DB.Exec("DROP TABLE " + tc.table); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				response := httptest.NewRecorder()
				tc.handler(response, httptest.NewRequest(method, tc.path, strings.NewReader(tc.body)))
				if response.Code != http.StatusInternalServerError {
					t.Fatalf("%s on missing %s table: got %d %s", method, tc.table, response.Code, response.Body.String())
				}
			}
		})
	}
}
