package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These faults are injected only into disposable test databases. Historical
// release backups still require separate migration acceptance testing.
func TestAppliedV7SchemaGuardPreservesUserDataAndRecoversAfterExplicitRepair(t *testing.T) {
	cases := []struct {
		name, damage, repair, valueColumn string
	}{
		{
			name: "missing_values_column",
			damage: "ALTER TABLE db_values RENAME COLUMN value TO legacy_value",
			repair: "ALTER TABLE db_values RENAME COLUMN legacy_value TO value",
			valueColumn: "legacy_value",
		},
		{
			name: "missing_views_table",
			damage: "ALTER TABLE db_views RENAME TO legacy_db_views",
			repair: "ALTER TABLE legacy_db_views RENAME TO db_views",
			valueColumn: "value",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupTestContext(t)
			root := app.RootPath
			database, record := dbRegressionCreate(t, app)
			project, err := app.createProject("Preserve project on damaged v7 schema")
			if err != nil {
				t.Fatal(err)
			}
			const taskID = "v7-schema-preserve-task"
			if _, err := app.DB.Exec("INSERT INTO tasks(id,project_id,title) VALUES(?,?,?)",
				taskID, project.ID, "Preserve task"); err != nil {
				t.Fatal(err)
			}
			var propertyID string
			if err := app.DB.QueryRow("SELECT id FROM db_properties WHERE database_id=? LIMIT 1",
				database.ID).Scan(&propertyID); err != nil {
				t.Fatal(err)
			}
			const savedValue = `"preserved v7 value"`
			if _, err := app.DB.Exec(`INSERT INTO db_values(record_id,property_id,value)
				VALUES(?,?,?) ON CONFLICT(record_id,property_id)
				DO UPDATE SET value=excluded.value`, record.ID, propertyID, savedValue); err != nil {
				t.Fatal(err)
			}
			providerPath := filepath.Join(root, "config", "providers.json")
			providerBefore, err := os.ReadFile(providerPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.DB.Exec(tc.damage); err != nil {
				t.Fatalf("damage disposable schema: %v", err)
			}
			if err := app.DB.Close(); err != nil {
				t.Fatal(err)
			}

			opened, err := initializeDatabase(root)
			if err == nil {
				opened.Close()
				t.Fatal("startup accepted a damaged applied v7 migration")
			}
			if !strings.Contains(err.Error(), "migration 7 is marked applied") {
				t.Fatalf("unexpected schema guard error: %v", err)
			}

			raw, err := sql.Open("sqlite", filepath.Join(root, "data", "planner.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			var got string
			for _, check := range []struct {
				query, id, want string
			}{
				{"SELECT title FROM db_records WHERE id=?", record.ID, record.Title},
				{"SELECT title FROM databases WHERE id=?", database.ID, database.Title},
				{"SELECT name FROM projects WHERE id=?", project.ID, "Preserve project on damaged v7 schema"},
				{"SELECT title FROM tasks WHERE id=?", taskID, "Preserve task"},
			} {
				if err := raw.QueryRow(check.query, check.id).Scan(&got); err != nil || got != check.want {
					t.Fatalf("startup changed user data: query=%s got=%q want=%q err=%v",
						check.query, got, check.want, err)
				}
			}
			if err := raw.QueryRow("SELECT "+tc.valueColumn+" FROM db_values WHERE record_id=? AND property_id=?",
				record.ID, propertyID).Scan(&got); err != nil || got != savedValue {
				t.Fatalf("startup changed stored value: got=%q err=%v", got, err)
			}
			var count int
			if err := raw.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=7").Scan(&count); err != nil || count != 1 {
				t.Fatalf("startup changed v7 migration marker: count=%d err=%v", count, err)
			}
			providerAfter, err := os.ReadFile(providerPath)
			if err != nil || !bytes.Equal(providerBefore, providerAfter) {
				t.Fatalf("startup changed AI Provider configuration: %v", err)
			}

			// Only the disposable fixture is explicitly repaired.
			if _, err := raw.Exec(tc.repair); err != nil {
				t.Fatalf("repair disposable schema: %v", err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			recovered, err := initializeDatabase(root)
			if err != nil {
				t.Fatalf("startup after explicit fixture repair: %v", err)
			}
			defer recovered.Close()
			if err := recovered.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
				record.ID, propertyID).Scan(&got); err != nil || got != savedValue {
				t.Fatalf("recovery lost stored value: got=%q err=%v", got, err)
			}
			if err := recovered.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=7").Scan(&count); err != nil || count != 1 {
				t.Fatalf("recovery lost v7 migration marker: count=%d err=%v", count, err)
			}
			providerAfter, err = os.ReadFile(providerPath)
			if err != nil || !bytes.Equal(providerBefore, providerAfter) {
				t.Fatalf("recovery changed AI Provider configuration: %v", err)
			}
		})
	}
}
