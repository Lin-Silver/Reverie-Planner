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

func TestLabelStateMutationsRejectInvalidWritesWithoutDataLoss(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	const projectID = "label-state-regression-project"
	if _, err := app.DB.Exec(`INSERT INTO projects (id, name, status, created_at) VALUES (?,?,?,?)`,
		projectID, "Keep project", "active", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		handler(res, httptest.NewRequest(method, path, strings.NewReader(body)))
		return res
	}
	createdLabel := request(http.MethodPost, "/api/labels",
		fmt.Sprintf(`{"projectId":%q,"name":"Keep label","color":"blue"}`, projectID), app.handleLabels)
	if createdLabel.Code != http.StatusCreated {
		t.Fatalf("create label: %d %s", createdLabel.Code, createdLabel.Body.String())
	}
	var label Label
	if err := json.Unmarshal(createdLabel.Body.Bytes(), &label); err != nil || label.ID == "" {
		t.Fatalf("decode label: %+v err=%v", label, err)
	}
	createdState := request(http.MethodPost, "/api/states",
		fmt.Sprintf(`{"projectId":%q,"name":"Keep state","group":"started","color":"green"}`, projectID), app.handleStates)
	if createdState.Code != http.StatusCreated {
		t.Fatalf("create state: %d %s", createdState.Code, createdState.Body.String())
	}
	var state State
	if err := json.Unmarshal(createdState.Body.Bytes(), &state); err != nil || state.ID == "" {
		t.Fatalf("decode state: %+v err=%v", state, err)
	}

	providersPath := filepath.Join(app.RootPath, "config", "providers.json")
	providersBefore, err := os.ReadFile(providersPath)
	if err != nil {
		t.Fatal(err)
	}
	checkUnchanged := func(t *testing.T) {
		t.Helper()
		for _, table := range []string{"labels", "states"} {
			var count int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
				t.Fatalf("%s count changed: count=%d err=%v", table, count, err)
			}
		}
		var labelName, labelColor, labelProject string
		if err := app.DB.QueryRow(`SELECT name,color,project_id FROM labels WHERE id=?`, label.ID).
			Scan(&labelName, &labelColor, &labelProject); err != nil ||
			labelName != "Keep label" || labelColor != "blue" || labelProject != projectID {
			t.Fatalf("label changed: name=%q color=%q project=%q err=%v", labelName, labelColor, labelProject, err)
		}
		var stateName, stateColor, stateGroup, stateProject string
		if err := app.DB.QueryRow(`SELECT name,color,group_name,project_id FROM states WHERE id=?`, state.ID).
			Scan(&stateName, &stateColor, &stateGroup, &stateProject); err != nil ||
			stateName != "Keep state" || stateColor != "green" || stateGroup != "started" || stateProject != projectID {
			t.Fatalf("state changed: name=%q color=%q group=%q project=%q err=%v",
				stateName, stateColor, stateGroup, stateProject, err)
		}
		var projectName string
		if err := app.DB.QueryRow(`SELECT name FROM projects WHERE id=?`, projectID).Scan(&projectName); err != nil || projectName != "Keep project" {
			t.Fatalf("project changed: name=%q err=%v", projectName, err)
		}
		after, err := os.ReadFile(providersPath)
		if err != nil || !bytes.Equal(providersBefore, after) {
			t.Fatalf("provider configuration changed: %v", err)
		}
	}
	cases := []struct {
		name, method, path, body string
		handler func(http.ResponseWriter, *http.Request)
		status int
	}{
		{"label trailing object", http.MethodPost, "/api/labels", `{"name":"Bad"}{"name":"Other"}`, app.handleLabels, 400},
		{"label duplicate field", http.MethodPost, "/api/labels", `{"name":"Bad","name":"Other"}`, app.handleLabels, 400},
		{"label null", http.MethodPost, "/api/labels", `null`, app.handleLabels, 400},
		{"label array", http.MethodPost, "/api/labels", `[{"name":"Bad"}]`, app.handleLabels, 400},
		{"label oversized", http.MethodPost, "/api/labels", fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 4<<20)), app.handleLabels, 400},
		{"label blank name", http.MethodPost, "/api/labels", `{"name":"   "}`, app.handleLabels, 400},
		{"label duplicate id", http.MethodPost, "/api/labels", fmt.Sprintf(`{"id":%q,"name":"Duplicate"}`, label.ID), app.handleLabels, 500},
		{"label invalid project", http.MethodPost, "/api/labels", `{"projectId":"nonexistent","name":"Bad"}`, app.handleLabels, 500},
		{"state trailing object", http.MethodPost, "/api/states", fmt.Sprintf(`{"projectId":%q,"name":"Bad"}{"name":"Other"}`, projectID), app.handleStates, 400},
		{"state duplicate field", http.MethodPost, "/api/states", fmt.Sprintf(`{"projectId":%q,"name":"Bad","name":"Other"}`, projectID), app.handleStates, 400},
		{"state malformed", http.MethodPost, "/api/states", `{"name":`, app.handleStates, 400},
		{"state wrong type", http.MethodPost, "/api/states", fmt.Sprintf(`{"projectId":%q,"name":3}`, projectID), app.handleStates, 400},
		{"state missing project", http.MethodPost, "/api/states", `{"name":"Bad"}`, app.handleStates, 400},
		{"state blank name", http.MethodPost, "/api/states", fmt.Sprintf(`{"projectId":%q,"name":"  "}`, projectID), app.handleStates, 400},
		{"state duplicate id", http.MethodPost, "/api/states", fmt.Sprintf(`{"id":%q,"projectId":%q,"name":"Duplicate"}`, state.ID, projectID), app.handleStates, 500},
		{"state invalid project", http.MethodPost, "/api/states", `{"projectId":"nonexistent","name":"Bad"}`, app.handleStates, 500},
		{"label delete without id", http.MethodDelete, "/api/labels", "", app.handleLabels, 400},
		{"state delete without id", http.MethodDelete, "/api/states", "", app.handleStates, 400},
		{"label delete missing", http.MethodDelete, "/api/labels?id=missing", "", app.handleLabels, 404},
		{"state delete missing", http.MethodDelete, "/api/states?id=missing", "", app.handleStates, 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.method, tc.path, tc.body, tc.handler)
			if res.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, res.Code, res.Body.String())
			}
			checkUnchanged(t)
		})
	}
	for _, tc := range []struct {
		path string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"/api/labels?id=" + label.ID, app.handleLabels},
		{"/api/states?id=" + state.ID, app.handleStates},
	} {
		res := request(http.MethodDelete, tc.path, "", tc.handler)
		if res.Code != http.StatusNoContent {
			t.Fatalf("valid delete %s: %d %s", tc.path, res.Code, res.Body.String())
		}
	}
	for _, table := range []string{"labels", "states"} {
		var count int
		if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("valid delete did not persist for %s: count=%d err=%v", table, count, err)
		}
	}
	after, err := os.ReadFile(providersPath)
	if err != nil || !bytes.Equal(providersBefore, after) {
		t.Fatalf("valid deletes changed provider configuration: %v", err)
	}
}
