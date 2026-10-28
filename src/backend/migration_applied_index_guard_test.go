package main

import (
    "bytes"
    "database/sql"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

// Every schema fault below is confined to a disposable test workspace. Do not
// attempt to repair a user's existing database automatically.
func TestAppliedMigrationIndexGuardPreservesDataAndRecoversAfterRepair(t *testing.T) {
    cases := []struct {
        name string
        version int
        index string
        expectedFlag string
        damage []string
        repair []string
    }{
        {
            name: "v8_missing_relation_lookup_index",
            version: 8,
            index: "idx_db_values_property",
            damage: []string{"DROP INDEX idx_db_values_property"},
            repair: []string{"CREATE INDEX idx_db_values_property ON db_values(property_id)"},
        },
        {
            name: "v9_wrong_live_record_index_columns",
            version: 9,
            index: "idx_db_records_live",
            damage: []string{
                "DROP INDEX idx_db_records_live",
                "CREATE INDEX idx_db_records_live ON db_records(database_id, created_at)",
            },
            repair: []string{
                "DROP INDEX idx_db_records_live",
                "CREATE INDEX idx_db_records_live ON db_records(database_id, deleted_at, created_at)",
            },
        },
        {
            name: "v10_missing_attachment_lookup_index",
            version: 10,
            index: "idx_db_files_record",
            damage: []string{"DROP INDEX idx_db_files_record"},
            repair: []string{"CREATE INDEX idx_db_files_record ON db_files(record_id, deleted_at)"},
        },
        {
            name: "v8_partial_property_lookup_index",
            version: 8,
            index: "idx_db_values_property",
            expectedFlag: "partial=1",
            damage: []string{
                "DROP INDEX idx_db_values_property",
                "CREATE INDEX idx_db_values_property ON db_values(property_id) WHERE value IS NOT NULL",
            },
            repair: []string{
                "DROP INDEX idx_db_values_property",
                "CREATE INDEX idx_db_values_property ON db_values(property_id)",
            },
        },
        {
            name: "v10_unexpected_unique_attachment_index",
            version: 10,
            index: "idx_db_files_database",
            expectedFlag: "unique=1",
            damage: []string{
                "DROP INDEX idx_db_files_database",
                "CREATE UNIQUE INDEX idx_db_files_database ON db_files(database_id)",
            },
            repair: []string{
                "DROP INDEX idx_db_files_database",
                "CREATE INDEX idx_db_files_database ON db_files(database_id)",
            },
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            app := setupTestContext(t)
            root := app.RootPath
            database, record := dbRegressionCreate(t, app)
            project, err := app.createProject("Preserve project across index guard")
            if err != nil {
                t.Fatal(err)
            }
            const taskID = "index-guard-task"
            if _, err := app.DB.Exec("INSERT INTO tasks(id,project_id,title) VALUES(?,?,?)",
                taskID, project.ID, "Preserve task across index guard"); err != nil {
                t.Fatal(err)
            }
            var propertyID string
            if err := app.DB.QueryRow("SELECT id FROM db_properties WHERE database_id=? LIMIT 1",
                database.ID).Scan(&propertyID); err != nil {
                t.Fatal(err)
            }
            const storedValue = `"preserve indexed value"`
            if _, err := app.DB.Exec(`INSERT INTO db_values(record_id,property_id,value)
                VALUES(?,?,?) ON CONFLICT(record_id,property_id)
                DO UPDATE SET value=excluded.value`, record.ID, propertyID, storedValue); err != nil {
                t.Fatal(err)
            }
            providerPath := filepath.Join(root, "config", "providers.json")
            providerBefore, err := os.ReadFile(providerPath)
            if err != nil {
                t.Fatal(err)
            }
            for _, statement := range tc.damage {
                if _, err := app.DB.Exec(statement); err != nil {
                    t.Fatalf("damage disposable index: %v", err)
                }
            }
            if err := app.DB.Close(); err != nil {
                t.Fatal(err)
            }

            opened, err := initializeDatabase(root)
            if err == nil {
                opened.Close()
                t.Fatal("startup accepted a damaged applied migration index")
            }
            if !strings.Contains(err.Error(), fmt.Sprintf("migration %d is marked applied", tc.version)) ||
                !strings.Contains(err.Error(), tc.index) ||
                (tc.expectedFlag != "" && !strings.Contains(err.Error(), tc.expectedFlag)) {
                t.Fatalf("unexpected index integrity diagnostic: %v", err)
            }

            raw, err := sql.Open("sqlite", filepath.Join(root, "data", "planner.db"))
            if err != nil {
                t.Fatal(err)
            }
            defer raw.Close()
            for _, check := range []struct {
                query, id, want string
            }{
                {"SELECT title FROM databases WHERE id=?", database.ID, database.Title},
                {"SELECT title FROM db_records WHERE id=?", record.ID, record.Title},
                {"SELECT name FROM projects WHERE id=?", project.ID, "Preserve project across index guard"},
                {"SELECT title FROM tasks WHERE id=?", taskID, "Preserve task across index guard"},
            } {
                var got string
                if err := raw.QueryRow(check.query, check.id).Scan(&got); err != nil || got != check.want {
                    t.Fatalf("failed startup changed user data: query=%q got=%q want=%q err=%v",
                        check.query, got, check.want, err)
                }
            }
            var value string
            if err := raw.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",
                record.ID, propertyID).Scan(&value); err != nil || value != storedValue {
                t.Fatalf("failed startup changed property value: got=%q err=%v", value, err)
            }
            var applied int
            if err := raw.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?",
                tc.version).Scan(&applied); err != nil || applied != 1 {
                t.Fatalf("failed startup changed migration marker: version=%d count=%d err=%v",
                    tc.version, applied, err)
            }
            providerAfter, err := os.ReadFile(providerPath)
            if err != nil || !bytes.Equal(providerBefore, providerAfter) {
                t.Fatalf("failed startup changed AI Provider configuration: %v", err)
            }

            for _, statement := range tc.repair {
                if _, err := raw.Exec(statement); err != nil {
                    t.Fatalf("repair disposable index: %v", err)
                }
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
                record.ID, propertyID).Scan(&value); err != nil || value != storedValue {
                t.Fatalf("recovery lost property value: got=%q err=%v", value, err)
            }
            providerAfter, err = os.ReadFile(providerPath)
            if err != nil || !bytes.Equal(providerBefore, providerAfter) {
                t.Fatalf("recovery changed AI Provider configuration: %v", err)
            }
        })
    }
}
