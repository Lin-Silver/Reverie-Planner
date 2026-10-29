package main

import (
    "bytes"
    "database/sql"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

// Synthetic damage is applied only to disposable test databases. This does not
// substitute for migration acceptance using actual historical user backups.
func TestAppliedMigrationMarkerRejectsIncompleteSchemaWithoutChangingUserData(t *testing.T) {
    cases := []struct {
        name string
        version int
        damage []string
        restoredColumnQuery string
    }{
        {"linked_page_column", 8, []string{
            "ALTER TABLE pages DROP COLUMN linked_view_id",
        }, "SELECT linked_view_id FROM pages LIMIT 1"},
        {"record_trash_column", 9, []string{
            "DROP INDEX idx_db_records_live",
            "ALTER TABLE db_records DROP COLUMN deleted_at",
        }, "SELECT deleted_at FROM db_records LIMIT 1"},
        {"file_table", 10, []string{
            "DROP TABLE db_files",
        }, "SELECT id FROM db_files LIMIT 1"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            app := setupTestContext(t)
            root := app.RootPath
            database, record := dbRegressionCreate(t, app)
            project, err := app.createProject("Preserve project during schema validation")
            if err != nil { t.Fatal(err) }
            const taskID = "migration-marker-task"
            if _, err := app.DB.Exec("INSERT INTO tasks(id,project_id,title) VALUES(?,?,?)", taskID, project.ID, "Preserve task"); err != nil { t.Fatal(err) }
            providerPath := filepath.Join(root, "config", "providers.json")
            providerBefore, err := os.ReadFile(providerPath)
            if err != nil { t.Fatal(err) }
            if tc.version == 8 { removePageRecoveryTestSchema(t,app.DB) }
            for _, stmt := range tc.damage {
                if _, err := app.DB.Exec(stmt); err != nil { t.Fatalf("prepare damaged fixture (%s): %v", stmt, err) }
            }
            if err := app.DB.Close(); err != nil { t.Fatal(err) }

            opened, err := initializeDatabase(root)
            if err == nil {
                opened.Close()
                t.Fatal("startup accepted a migration marker with missing schema")
            }
            if !strings.Contains(err.Error(), "migration ") || !strings.Contains(err.Error(), "marked applied") {
                t.Fatalf("startup did not explain schema mismatch: %v", err)
            }

            raw, err := sql.Open("sqlite", filepath.Join(root, "data", "planner.db"))
            if err != nil { t.Fatal(err) }
            var got string
            if err := raw.QueryRow("SELECT title FROM db_records WHERE id=?", record.ID).Scan(&got); err != nil || got != record.Title {
                t.Fatalf("failed startup changed record: %q err=%v", got, err)
            }
            if err := raw.QueryRow("SELECT title FROM databases WHERE id=?", database.ID).Scan(&got); err != nil || got != database.Title {
                t.Fatalf("failed startup changed database: %q err=%v", got, err)
            }
            if err := raw.QueryRow("SELECT title FROM tasks WHERE id=?", taskID).Scan(&got); err != nil || got != "Preserve task" {
                t.Fatalf("failed startup changed task: %q err=%v", got, err)
            }
            if err := raw.QueryRow("SELECT name FROM projects WHERE id=?", project.ID).Scan(&got); err != nil || got != "Preserve project during schema validation" {
                t.Fatalf("failed startup changed project: %q err=%v", got, err)
            }
            var count int
            if err := raw.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", tc.version).Scan(&count); err != nil || count != 1 {
                t.Fatalf("failed startup changed migration %d marker: count=%d err=%v", tc.version, count, err)
            }
            providerAfter, err := os.ReadFile(providerPath)
            if err != nil || !bytes.Equal(providerBefore, providerAfter) {
                t.Fatalf("failed startup changed AI Provider configuration: %v", err)
            }

            // Explicit fixture repair only. Production startup never deletes
            // migration markers or silently reconstructs applied migrations.
            if _, err := raw.Exec("DELETE FROM schema_migrations WHERE version=?", tc.version); err != nil { t.Fatal(err) }
            if err := raw.Close(); err != nil { t.Fatal(err) }
            repaired, err := initializeDatabase(root)
            if err != nil { t.Fatalf("restart after explicit fixture repair: %v", err) }
            defer repaired.Close()
            rows, err := repaired.Query(tc.restoredColumnQuery)
            if err != nil { t.Fatalf("required column/table was not restored: %v", err) }
            if err := rows.Close(); err != nil { t.Fatal(err) }
            if err := repaired.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", tc.version).Scan(&count); err != nil || count != 1 {
                t.Fatalf("migration %d not reapplied: count=%d err=%v", tc.version, count, err)
            }
            if err := repaired.QueryRow("SELECT title FROM db_records WHERE id=?", record.ID).Scan(&got); err != nil || got != record.Title {
                t.Fatalf("recovery lost record: %q err=%v", got, err)
            }
        })
    }
}
