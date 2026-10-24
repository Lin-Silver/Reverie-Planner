package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestContext(t *testing.T) *AppContext {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := AppConfig{AppName: "Reverie Planner", Version: appVersion, Port: 8765, Host: "127.0.0.1", Theme: "dark"}
	cfg.Features.AIEnabled = true
	cfg.Features.LocalOnly = true
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "app.json"), cfgBytes, 0o644); err != nil {
		t.Fatalf("write app config: %v", err)
	}
	if err := ensureProviderConfig(root); err != nil {
		t.Fatalf("provider config: %v", err)
	}
	db, err := initializeDatabase(root)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	return &AppContext{DB: db, RootPath: root, Config: cfg}
}

func TestProjectTaskCrudLifecycle(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	if err := seedDemoData(ctx); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	project := Project{Name: "Alpha", Description: "Test project", Status: "active", Owner: "Tester", Color: "cyan"}
	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(string(body)))
	res := httptest.NewRecorder()
	ctx.handleProjects(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("unexpected project create status: %d %s", res.Code, res.Body.String())
	}

	projects, err := ctx.fetchProjects()
	if err != nil {
		t.Fatalf("fetch projects: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("expected at least one project")
	}

	task := Task{ProjectID: projects[0].ID, Title: "Write tests", Status: "todo", Priority: "high", Assignee: "Tester"}
	body, err = json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(string(body)))
	res = httptest.NewRecorder()
	ctx.handleTasks(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("unexpected task create status: %d %s", res.Code, res.Body.String())
	}

	tasks, err := ctx.listTasks(projects[0].ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected a created task")
	}
	updated := tasks[0]
	updated.Status = "done"
	body, err = json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal updated task: %v", err)
	}
	req = httptest.NewRequest(http.MethodPut, "/api/tasks", strings.NewReader(string(body)))
	res = httptest.NewRecorder()
	ctx.handleTasks(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected task update status: %d %s", res.Code, res.Body.String())
	}

	if _, err := ctx.taskByID(updated.ID); err != nil {
		t.Fatalf("task lookup failed: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/tasks?id="+updated.ID, nil)
	res = httptest.NewRecorder()
	ctx.handleTasks(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("task delete status expected 204 got %d: %s", res.Code, res.Body.String())
	}
}

func TestAIChatHandlesLocalToolCommand(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	if err := seedDemoData(ctx); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", strings.NewReader(`{"message":"create project Release QA"}`))
	res := httptest.NewRecorder()
	ctx.handleAIChat(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("ai chat status = %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "Release QA") {
		t.Fatalf("ai chat response did not include created project: %s", res.Body.String())
	}
}
