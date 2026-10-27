package main

import (
    "bytes"
    "net/http"
    "os"
    "path/filepath"
    "testing"
)

// A missing trash row is a 404, but a failed SQLite query must never be
// disguised as a missing item or a dependency conflict.
func TestTrashRestoreReportsSQLiteReadFailuresWithoutChangingTombstones(t *testing.T) {
    cases := []struct {
        name, kind, brokenTable string
    }{
        {"database lookup", "database", "databases"},
        {"record lookup", "record", "db_records"},
        {"record parent lookup", "record", "databases"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            app := setupTestContext(t)
            defer app.DB.Close()
            database, record := dbRegressionCreate(t, app)
            id, table := database.ID, "databases"
            deletePath := "/api/databases?id=" + id
            deleteHandler := app.handleDatabases
            if tc.kind == "record" {
                id, table = record.ID, "db_records"
                deletePath = "/api/db/records?id=" + id
                deleteHandler = app.handleDBRecords
            }
            removed := dbRegressionCall(t, app, http.MethodDelete, deletePath, "", deleteHandler)
            if removed.Code != http.StatusNoContent {
                t.Fatalf("trash item: %d %s", removed.Code, removed.Body.String())
            }
            var beforeVersion int
            var beforeDeleted string
            if err := app.DB.QueryRow("SELECT version,deleted_at FROM "+table+" WHERE id=?", id).
                Scan(&beforeVersion, &beforeDeleted); err != nil { t.Fatal(err) }

            providerPath := filepath.Join(app.RootPath, "config", "providers.json")
            beforeProvider, err := os.ReadFile(providerPath)
            if err != nil { t.Fatal(err) }
            if _, err := app.DB.Exec("ALTER TABLE "+tc.brokenTable+
                " RENAME COLUMN deleted_at TO deleted_at_unavailable"); err != nil { t.Fatal(err) }

            payload := `{"kind":"` + tc.kind + `","id":"` + id + `"}`
            failed := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash", payload, app.handleDBTrash)
            if failed.Code != http.StatusInternalServerError {
                t.Fatalf("SQLite read failure was misclassified: %d %s", failed.Code, failed.Body.String())
            }
            if _, err := app.DB.Exec("ALTER TABLE "+tc.brokenTable+
                " RENAME COLUMN deleted_at_unavailable TO deleted_at"); err != nil { t.Fatal(err) }
            var afterVersion int
            var afterDeleted string
            if err := app.DB.QueryRow("SELECT version,deleted_at FROM "+table+" WHERE id=?", id).
                Scan(&afterVersion, &afterDeleted); err != nil { t.Fatal(err) }
            if afterVersion != beforeVersion || afterDeleted != beforeDeleted {
                t.Fatalf("failed restore modified tombstone: (%d,%q) -> (%d,%q)",
                    beforeVersion, beforeDeleted, afterVersion, afterDeleted)
            }
            afterProvider, err := os.ReadFile(providerPath)
            if err != nil || !bytes.Equal(beforeProvider, afterProvider) {
                t.Fatalf("provider configuration changed: %v", err)
            }
            missing := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash",
                `{"kind":"`+tc.kind+`","id":"does-not-exist"}`, app.handleDBTrash)
            if missing.Code != http.StatusNotFound {
                t.Fatalf("actual missing item should be 404: %d %s", missing.Code, missing.Body.String())
            }
            restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash", payload, app.handleDBTrash)
            if restored.Code != http.StatusOK {
                t.Fatalf("restore after repairing disposable schema: %d %s", restored.Code, restored.Body.String())
            }
            if err := app.DB.QueryRow("SELECT version FROM "+table+" WHERE id=?", id).Scan(&afterVersion); err != nil { t.Fatal(err) }
            if afterVersion != beforeVersion+1 {
                t.Fatalf("successful restore should increment version once: %d -> %d", beforeVersion, afterVersion)
            }
        })
    }
}

func TestTrashRestoreRollsBackOnSQLiteWriteFailure(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    _, record := dbRegressionCreate(t, app)
    removed := dbRegressionCall(t, app, http.MethodDelete,
        "/api/db/records?id="+record.ID, "", app.handleDBRecords)
    if removed.Code != http.StatusNoContent { t.Fatalf("trash record: %d %s", removed.Code, removed.Body.String()) }

    var beforeVersion int
    var beforeDeleted string
    if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?", record.ID).
        Scan(&beforeVersion, &beforeDeleted); err != nil { t.Fatal(err) }
    if _, err := app.DB.Exec(`CREATE TRIGGER block_trash_restore BEFORE UPDATE OF deleted_at ON db_records
        BEGIN SELECT RAISE(ABORT, 'simulated restore write failure'); END`); err != nil { t.Fatal(err) }
    payload := `{"kind":"record","id":"` + record.ID + `"}`
    failed := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash", payload, app.handleDBTrash)
    if failed.Code != http.StatusInternalServerError {
        t.Fatalf("failed SQL update should be 500: %d %s", failed.Code, failed.Body.String())
    }
    var afterVersion int
    var afterDeleted string
    if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?", record.ID).
        Scan(&afterVersion, &afterDeleted); err != nil { t.Fatal(err) }
    if afterVersion != beforeVersion || afterDeleted != beforeDeleted {
        t.Fatalf("failed SQL update changed tombstone/version: (%d,%q) -> (%d,%q)",
            beforeVersion, beforeDeleted, afterVersion, afterDeleted)
    }
    if _, err := app.DB.Exec("DROP TRIGGER block_trash_restore"); err != nil { t.Fatal(err) }
    restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash", payload, app.handleDBTrash)
    if restored.Code != http.StatusOK {
        t.Fatalf("valid restore after trigger removal: %d %s", restored.Code, restored.Body.String())
    }
}
