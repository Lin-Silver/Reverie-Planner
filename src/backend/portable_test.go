package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPortableRootArguments(t *testing.T) {
	original := os.Args
	defer func() { os.Args = original }()
	root := filepath.Join(t.TempDir(), "workspace with spaces")
	for _, args := range [][]string{{"planner", "--root=" + root}, {"planner", "--stop", "--root", root}} {
		os.Args = args
		got, err := resolveRootPath()
		if err != nil || got != root {
			t.Fatalf("%v: %q %v", args, got, err)
		}
	}
	for _, args := range [][]string{{"planner", "--root"}, {"planner", "--root="}, {"planner", "--root", "--stop"}} {
		os.Args = args
		if _, err := resolveRootPath(); err == nil {
			t.Fatalf("accepted missing directory: %v", args)
		}
	}
}
func TestWorkspaceLocationsContainNoCredentials(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	res := httptest.NewRecorder()
	app.handleWorkspace(res, httptest.NewRequest(http.MethodGet, "/api/workspace", nil))
	var data map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data) != 5 || data["version"] != "0.0.1" || data["attachments"] != filepath.Join(app.RootPath, "data", "attachments") {
		t.Fatalf("unexpected workspace metadata: %v", data)
	}
}
