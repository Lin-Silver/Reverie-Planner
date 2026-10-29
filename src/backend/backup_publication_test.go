package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// Verify failure never advertises a partial backup, and preserves earlier
// successful backups and the original SQLite database.
func TestBackupCancellationPreservesPublishedSnapshotsAndSource(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	const marker = "backup-cancellation-marker"
	if _, err := app.DB.Exec("INSERT INTO app_state(key, value) VALUES (?, ?)", marker, "untouched"); err != nil {
		t.Fatal(err)
	}
	first, err := createDatabaseBackup(context.Background(), app.DB, app.RootPath)
	if err != nil {
		t.Fatalf("create first backup: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path, err := createDatabaseBackup(ctx, app.DB, app.RootPath)
	if err == nil || path != "" {
		t.Fatalf("cancelled backup returned path=%q err=%v", path, err)
	}
	entries, err := os.ReadDir(filepath.Dir(first))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(first) || entries[0].IsDir() {
		t.Fatalf("cancelled backup left unpublished files/directories: %+v", entries)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("existing published backup lost: %v", err)
	}
	var value string
	if err := app.DB.QueryRow("SELECT value FROM app_state WHERE key=?", marker).Scan(&value); err != nil || value != "untouched" {
		t.Fatalf("backup cancellation affected live data: value=%q err=%v", value, err)
	}
}

// A physically consistent SQLite file may still contain dangling foreign
// keys. Such a snapshot must not be published as a successful backup.
func TestBackupRejectsBrokenForeignKeysWithoutChangingUserData(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providersBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec("INSERT INTO tasks (id,project_id,title) VALUES (?,?,?)", "backup-orphan", "absent-project", "Keep orphan for repair"); err != nil {
		t.Fatalf("create disposable broken reference: %v", err)
	}
	path, err := createDatabaseBackup(context.Background(), app.DB, app.RootPath)
	if err == nil || path != "" {
		t.Fatalf("backup accepted broken foreign keys: path=%q err=%v", path, err)
	}
	entries, err := os.ReadDir(filepath.Join(app.RootPath, "data", "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("backup published incomplete/invalid artifacts: %+v", entries)
	}
	var title string
	if err := app.DB.QueryRow("SELECT title FROM tasks WHERE id='backup-orphan'").Scan(&title); err != nil || title != "Keep orphan for repair" {
		t.Fatalf("failure changed source row: title=%q err=%v", title, err)
	}
	providersAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providersBefore, providersAfter) {
		t.Fatalf("backup changed AI provider config: err=%v", err)
	}
	// Only repair this disposable test fixture, then prove healthy snapshots
	// are still publishable and pass the same foreign-key check.
	if _, err := app.DB.Exec("DELETE FROM tasks WHERE id='backup-orphan'"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	good, err := createDatabaseBackup(context.Background(), app.DB, app.RootPath)
	if err != nil {
		t.Fatalf("healthy backup could not be published: %v", err)
	}
	snapshot, err := sql.Open("sqlite", good)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var check string
	if err := snapshot.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Fatalf("published backup is corrupt: %q %v", check, err)
	}
	rows, err := snapshot.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatal("published backup contains foreign key violations")
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
}
