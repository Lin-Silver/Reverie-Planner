package main

import (
    "bytes"
    "database/sql"
    "os"
    "path/filepath"
    "strings"
    "testing"

    _ "modernc.org/sqlite"
)

// A corrupted foreign-key reference must be reported, not silently repaired
// or deleted. The fixture lives entirely in a disposable test directory.
func TestDatabaseStartupRejectsOrphanWithoutDeletingUserData(t *testing.T) {
    app := setupTestContext(t)
    root := app.RootPath
    providerPath := filepath.Join(root, "config", "providers.json")
    providerBefore, err := os.ReadFile(providerPath)
    if err != nil { t.Fatal(err) }

    if _, err := app.DB.Exec("PRAGMA foreign_keys = OFF"); err != nil { t.Fatal(err) }
    if _, err := app.DB.Exec("INSERT INTO tasks(id, project_id, title) VALUES (?, ?, ?)", "orphan-task", "missing-project", "Do not delete me"); err != nil {
        t.Fatalf("construct orphaned task: %v", err)
    }
    if err := app.DB.Close(); err != nil { t.Fatal(err) }

    reopened, err := initializeDatabase(root)
    if err == nil {
        _ = reopened.Close()
        t.Fatal("startup accepted a database with an orphaned task")
    }
    if !strings.Contains(err.Error(), "foreign-key violation") || !strings.Contains(err.Error(), "tasks") {
        t.Fatalf("startup returned an unhelpful error: %v", err)
    }

    raw, err := sql.Open("sqlite", filepath.Join(root, "data", "planner.db"))
    if err != nil { t.Fatal(err) }
    var title string
    if err := raw.QueryRow("SELECT title FROM tasks WHERE id=?", "orphan-task").Scan(&title); err != nil || title != "Do not delete me" {
        t.Fatalf("startup changed or deleted the original task: title=%q err=%v", title, err)
    }
    providerAfter, err := os.ReadFile(providerPath)
    if err != nil || !bytes.Equal(providerBefore, providerAfter) {
        t.Fatalf("startup changed provider config: %v", err)
    }

    // After explicit repair of this disposable fixture, startup must work.
    if _, err := raw.Exec("DELETE FROM tasks WHERE id=?", "orphan-task"); err != nil { t.Fatal(err) }
    if err := raw.Close(); err != nil { t.Fatal(err) }
    healthy, err := initializeDatabase(root)
    if err != nil { t.Fatalf("startup after fixture repair: %v", err) }
    defer healthy.Close()
    var enabled int
    if err := healthy.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
        t.Fatalf("foreign keys not enforced: enabled=%d err=%v", enabled, err)
    }
    if _, err := healthy.Exec("INSERT INTO tasks(id, project_id, title) VALUES (?, ?, ?)", "new-orphan", "missing-project", "Must fail"); err == nil {
        t.Fatal("foreign-key enforcement accepted a new orphaned task")
    }
}
