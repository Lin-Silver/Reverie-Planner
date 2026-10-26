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

func TestModuleMutationsRejectInvalidWritesAndPreservePartialUpdates(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	const projectID = "module-regression-project"
	if _, err := app.DB.Exec(`INSERT INTO projects (id, name, status, created_at) VALUES (?,?,?,?)`,
		projectID, "Keep project", "active", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		app.handleModules(res, httptest.NewRequest(method, path, strings.NewReader(body)))
		return res
	}
	created := request(http.MethodPost, "/api/modules",
		fmt.Sprintf(`{"projectId":%q,"name":"Keep module","description":"Keep description","status":"active","lead":"Keep lead","startDate":"2026-10-01","endDate":"2026-10-31"}`, projectID))
	if created.Code != http.StatusCreated {
		t.Fatalf("create module: %d %s", created.Code, created.Body.String())
	}
	var module Module
	if err := json.Unmarshal(created.Body.Bytes(), &module); err != nil || module.ID == "" {
		t.Fatalf("decode created module: %+v err=%v", module, err)
	}
	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	beforeProviders, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	checkUnchanged := func(t *testing.T, expectedName, expectedLead string) {
		t.Helper()
		var id, project, name, status string
		var description, lead, start, end sqlNullString
		err := app.DB.QueryRow(`SELECT id,project_id,name,description,status,lead,start_date,end_date FROM modules WHERE id=?`, module.ID).
			Scan(&id, &project, &name, &description, &status, &lead, &start, &end)
		if err != nil || id != module.ID || project != projectID || name != expectedName ||
			description.String != "Keep description" || status != "active" || lead.String != expectedLead ||
			start.String != "2026-10-01" || end.String != "2026-10-31" {
			t.Fatalf("module changed: id=%q project=%q name=%q description=%q status=%q lead=%q dates=%q/%q err=%v",
				id, project, name, description, status, lead, start, end, err)
		}
		var count int
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM modules`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("module count=%d err=%v", count, err)
		}
		afterProviders, err := os.ReadFile(providerPath)
		if err != nil || !bytes.Equal(beforeProviders, afterProviders) {
			t.Fatalf("provider config changed: %v", err)
		}
	}
	invalid := []struct {
		name, method, path, body string
		status int
	}{
		{"trailing object", http.MethodPost, "/api/modules", `{"name":"Bad"}{}`, 400},
		{"duplicate field", http.MethodPost, "/api/modules", `{"name":"Bad","name":"Other"}`, 400},
		{"malformed", http.MethodPost, "/api/modules", `{"name":`, 400},
		{"wrong type", http.MethodPost, "/api/modules", `{"name":42}`, 400},
		{"null", http.MethodPost, "/api/modules", `null`, 400},
		{"array", http.MethodPost, "/api/modules", `[{"name":"Bad"}]`, 400},
		{"oversized", http.MethodPost, "/api/modules", fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 4<<20)), 400},
		{"missing project", http.MethodPost, "/api/modules", `{"name":"Bad"}`, 400},
		{"blank name", http.MethodPost, "/api/modules", fmt.Sprintf(`{"projectId":%q,"name":"  "}`, projectID), 400},
		{"invalid start date", http.MethodPost, "/api/modules", fmt.Sprintf(`{"projectId":%q,"name":"Bad","startDate":"2026-02-30"}`, projectID), 400},
		{"invalid end date", http.MethodPost, "/api/modules", fmt.Sprintf(`{"projectId":%q,"name":"Bad","endDate":"2026-13-01"}`, projectID), 400},
		{"duplicate id", http.MethodPost, "/api/modules", fmt.Sprintf(`{"id":%q,"projectId":%q,"name":"Bad"}`, module.ID, projectID), 500},
		{"invalid foreign key", http.MethodPost, "/api/modules", `{"projectId":"missing","name":"Bad"}`, 500},
		{"put trailing object", http.MethodPut, "/api/modules", fmt.Sprintf(`{"id":%q,"name":"Bad"}{}`, module.ID), 400},
		{"put duplicate", http.MethodPut, "/api/modules", fmt.Sprintf(`{"id":%q,"name":"Bad","name":"Other"}`, module.ID), 400},
		{"put wrong type", http.MethodPut, "/api/modules", fmt.Sprintf(`{"id":%q,"name":42}`, module.ID), 400},
		{"put blank name", http.MethodPut, "/api/modules", fmt.Sprintf(`{"id":%q,"name":"  "}`, module.ID), 400},
		{"put invalid date", http.MethodPut, "/api/modules", fmt.Sprintf(`{"id":%q,"endDate":"2026-02-30"}`, module.ID), 400},
		{"put project change", http.MethodPut, "/api/modules", fmt.Sprintf(`{"id":%q,"projectId":"other"}`, module.ID), 400},
		{"put missing id", http.MethodPut, "/api/modules", `{"name":"Bad"}`, 400},
		{"put unknown id", http.MethodPut, "/api/modules", `{"id":"missing","name":"Bad"}`, 404},
		{"delete missing id", http.MethodDelete, "/api/modules", "", 400},
		{"delete unknown id", http.MethodDelete, "/api/modules?id=missing", "", 404},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.method, tc.path, tc.body)
			if res.Code != tc.status {
				t.Fatalf("expected %d got %d: %s", tc.status, res.Code, res.Body.String())
			}
			checkUnchanged(t, "Keep module", "Keep lead")
		})
	}
	updated := request(http.MethodPut, "/api/modules", fmt.Sprintf(`{"id":%q,"name":"Renamed module"}`, module.ID))
	if updated.Code != http.StatusOK {
		t.Fatalf("partial update: %d %s", updated.Code, updated.Body.String())
	}
	checkUnchanged(t, "Renamed module", "Keep lead")

	cleared := request(http.MethodPut, "/api/modules", fmt.Sprintf(`{"id":%q,"lead":""}`, module.ID))
	if cleared.Code != http.StatusOK {
		t.Fatalf("explicit lead clear: %d %s", cleared.Code, cleared.Body.String())
	}
	checkUnchanged(t, "Renamed module", "")

	deleted := request(http.MethodDelete, "/api/modules?id="+module.ID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete module: %d %s", deleted.Code, deleted.Body.String())
	}
	var count int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM modules`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted module persisted: count=%d err=%v", count, err)
	}
	afterProviders, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(beforeProviders, afterProviders) {
		t.Fatalf("provider config changed after deletion: %v", err)
	}
}
