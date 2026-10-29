package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyTaskColumnUpgradePreservesExistingRowsAndProviderConfig(t *testing.T) {
	app := setupTestContext(t)
	root := app.RootPath
	project, err := app.createProject("Keep legacy project")
	if err != nil {
		t.Fatal(err)
	}
	const taskID = "legacy-task-column-regression"
	if _, err := app.DB.Exec("INSERT INTO tasks(id, project_id, title) VALUES(?,?,?)", taskID, project.ID, "Keep legacy task"); err != nil {
		t.Fatal(err)
	}
	providerPath := filepath.Join(root, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"labels_json", "deleted_at"} {
		if _, err := app.DB.Exec("ALTER TABLE tasks DROP COLUMN " + column); err != nil {
			t.Fatalf("construct partial legacy schema (%s): %v", column, err)
		}
	}
	if err := app.DB.Close(); err != nil {
		t.Fatal(err)
	}

	for restart := 0; restart < 2; restart++ {
		upgraded, err := initializeDatabase(root)
		if err != nil {
			t.Fatalf("restart %d: %v", restart, err)
		}
		var title, projectID string
		var labels, deleted sql.NullString
		err = upgraded.QueryRow("SELECT title, project_id, labels_json, deleted_at FROM tasks WHERE id=?", taskID).Scan(&title, &projectID, &labels, &deleted)
		if err != nil || title != "Keep legacy task" || projectID != project.ID || labels.Valid || deleted.Valid {
			t.Fatalf("restart %d changed task: title=%q project=%q labels=%v deleted=%v err=%v", restart, title, projectID, labels, deleted, err)
		}
		var projectName string
		if err := upgraded.QueryRow("SELECT name FROM projects WHERE id=?", project.ID).Scan(&projectName); err != nil || projectName != "Keep legacy project" {
			t.Fatalf("restart %d changed project: name=%q err=%v", restart, projectName, err)
		}
		if err := upgraded.Close(); err != nil {
			t.Fatal(err)
		}
	}
	providerAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, providerAfter) {
		t.Fatalf("legacy task migration changed AI Provider configuration: %v", err)
	}
}

func TestLegacyTaskSchemaFailureDoesNotSilentlyStartOrDestroyData(t *testing.T) {
	app := setupTestContext(t)
	root := app.RootPath
	project, err := app.createProject("Keep project after schema failure")
	if err != nil {
		t.Fatal(err)
	}
	const taskID = "legacy-task-schema-fault"
	if _, err := app.DB.Exec("INSERT INTO tasks(id, project_id, title) VALUES(?,?,?)", taskID, project.ID, "Preserve task through failure"); err != nil {
		t.Fatal(err)
	}
	providerPath := filepath.Join(root, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec("ALTER TABLE tasks RENAME TO tasks_staged"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec("CREATE VIEW tasks AS SELECT * FROM tasks_staged"); err != nil {
		t.Fatal(err)
	}
	if err := app.DB.Close(); err != nil {
		t.Fatal(err)
	}

	unexpected, err := initializeDatabase(root)
	if err == nil {
		unexpected.Close()
		t.Fatal("startup silently accepted a view in place of the tasks table")
	}
	if !strings.Contains(err.Error(), "tasks table") {
		t.Fatalf("startup returned an unhelpful task schema error: %v", err)
	}

	raw, err := sql.Open("sqlite", filepath.Join(root, "data", "planner.db"))
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err := raw.QueryRow("SELECT title FROM tasks_staged WHERE id=?", taskID).Scan(&title); err != nil || title != "Preserve task through failure" {
		t.Fatalf("failed startup changed existing task: title=%q err=%v", title, err)
	}
	providerAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, providerAfter) {
		t.Fatalf("failed startup changed AI Provider configuration: %v", err)
	}
	if _, err := raw.Exec("DROP VIEW tasks"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("ALTER TABLE tasks_staged RENAME TO tasks"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := initializeDatabase(root)
	if err != nil {
		t.Fatalf("startup after explicit fixture repair: %v", err)
	}
	defer recovered.Close()
	if err := recovered.QueryRow("SELECT title FROM tasks WHERE id=?", taskID).Scan(&title); err != nil || title != "Preserve task through failure" {
		t.Fatalf("recovery lost task: title=%q err=%v", title, err)
	}
}
