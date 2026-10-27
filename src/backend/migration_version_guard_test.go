package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFutureSchemaVersionRefusesStartupWithoutMigratingOrChangingUserData(t *testing.T) {
	for _, tc := range []struct {
		name             string
		missingMigration bool
	}{
		{name: "current_schema_with_future_version"},
		{name: "incomplete_schema_with_future_version", missingMigration: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := setupTestContext(t)
			root := app.RootPath
			project, err := app.createProject("Preserve project on unsupported schema")
			if err != nil {
				t.Fatal(err)
			}
			const taskID = "future-schema-preserve-task"
			if _, err := app.DB.Exec(
				"INSERT INTO tasks(id, project_id, title) VALUES(?,?,?)",
				taskID, project.ID, "Preserve task on unsupported schema",
			); err != nil {
				t.Fatal(err)
			}
			providerPath := filepath.Join(root, "config", "providers.json")
			providerBefore, err := os.ReadFile(providerPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := app.DB.Close(); err != nil {
				t.Fatal(err)
			}

			raw, err := sql.Open("sqlite", filepath.Join(root, "data", "planner.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			if tc.missingMigration {
				if _, err := raw.Exec("DELETE FROM schema_migrations WHERE version=10"); err != nil {
					t.Fatal(err)
				}
			}
			const futureVersion = latestSupportedSchemaMigration + 1
			if _, err := raw.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES(?,CURRENT_TIMESTAMP)", futureVersion); err != nil {
				t.Fatal(err)
			}

			opened, err := initializeDatabase(root)
			if err == nil {
				opened.Close()
				t.Fatal("older application accepted a future database schema")
			}
			if !strings.Contains(err.Error(), "newer than supported version") {
				t.Fatalf("unexpected future schema error: %v", err)
			}

			var taskTitle, projectID string
			if err := raw.QueryRow(
				"SELECT title, project_id FROM tasks WHERE id=?", taskID,
			).Scan(&taskTitle, &projectID); err != nil {
				t.Fatal(err)
			}
			if taskTitle != "Preserve task on unsupported schema" || projectID != project.ID {
				t.Fatalf("failed startup changed task: title=%q project=%q", taskTitle, projectID)
			}
			var projectName string
			if err := raw.QueryRow("SELECT name FROM projects WHERE id=?", project.ID).Scan(&projectName); err != nil || projectName != "Preserve project on unsupported schema" {
				t.Fatalf("failed startup changed project: name=%q err=%v", projectName, err)
			}
			var futureCount, migration10Count int
			if err := raw.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", futureVersion).Scan(&futureCount); err != nil {
				t.Fatal(err)
			}
			if err := raw.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=10").Scan(&migration10Count); err != nil {
				t.Fatal(err)
			}
			expected10 := 1
			if tc.missingMigration {
				expected10 = 0
			}
			if futureCount != 1 || migration10Count != expected10 {
				t.Fatalf("failed startup changed migration markers: future=%d version10=%d", futureCount, migration10Count)
			}
			providerAfter, err := os.ReadFile(providerPath)
			if err != nil || !bytes.Equal(providerBefore, providerAfter) {
				t.Fatalf("failed startup changed AI Provider configuration: %v", err)
			}

			// The test fixture is explicitly repaired; production user data is
			// never modified by the startup guard.
			if _, err := raw.Exec("DELETE FROM schema_migrations WHERE version=?", futureVersion); err != nil {
				t.Fatal(err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			recovered, err := initializeDatabase(root)
			if err != nil {
				t.Fatalf("startup after removing unsupported fixture marker: %v", err)
			}
			defer recovered.Close()
			if err := recovered.QueryRow("SELECT title FROM tasks WHERE id=?", taskID).Scan(&taskTitle); err != nil || taskTitle != "Preserve task on unsupported schema" {
				t.Fatalf("recovery lost task: title=%q err=%v", taskTitle, err)
			}
			if err := recovered.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=10").Scan(&migration10Count); err != nil || migration10Count != 1 {
				t.Fatalf("recovery did not complete supported migration: count=%d err=%v", migration10Count, err)
			}
		})
	}
}
