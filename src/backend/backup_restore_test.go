package main

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Test an offline SQLite-only restore to a separate root; never overwrite the live DB.
// Attachments and AI provider configuration are separate from SQLite backups.
func TestDatabaseBackupRestoresIntoIsolatedRootWithoutChangingSource(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	const key, saved = "restore-regression-marker", "persisted-value"
	if _, err := app.DB.Exec("INSERT INTO app_state (key, value) VALUES (?, ?)", key, saved); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	providersPath := filepath.Join(app.RootPath, "config", "providers.json")
	providersBefore, err := os.ReadFile(providersPath)
	if err != nil {
		t.Fatalf("read provider configuration: %v", err)
	}
	backupPath, err := createDatabaseBackup(context.Background(), app.DB, app.RootPath)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	restoredRoot := t.TempDir()
	restoredData := filepath.Join(restoredRoot, "data")
	if err := os.MkdirAll(restoredData, 0o700); err != nil {
		t.Fatalf("mkdir isolated data: %v", err)
	}
	restoredPath := filepath.Join(restoredData, "planner.db")
	source, err := os.Open(backupPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer source.Close()
	dest, err := os.OpenFile(restoredPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create isolated restore: %v", err)
	}
	if _, err := io.Copy(dest, source); err != nil {
		_ = dest.Close()
		t.Fatalf("copy backup: %v", err)
	}
	if err := dest.Sync(); err != nil {
		_ = dest.Close()
		t.Fatalf("sync restored database: %v", err)
	}
	if err := dest.Close(); err != nil {
		t.Fatalf("close restored database: %v", err)
	}

	restored, err := sql.Open("sqlite", restoredPath)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	defer restored.Close()
	var integrity string
	if err := restored.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("restored integrity: %q, %v", integrity, err)
	}
	var got string
	if err := restored.QueryRow("SELECT value FROM app_state WHERE key=?", key).Scan(&got); err != nil || got != saved {
		t.Fatalf("restored marker: %q, %v", got, err)
	}
	if _, err := restored.Exec("UPDATE app_state SET value=? WHERE key=?", "restored-only", key); err != nil {
		t.Fatalf("update restored database: %v", err)
	}
	if err := app.DB.QueryRow("SELECT value FROM app_state WHERE key=?", key).Scan(&got); err != nil || got != saved {
		t.Fatalf("restore mutated source: %q, %v", got, err)
	}
	// The restored snapshot must also survive the application's normal startup path.
	reopened, err := initializeDatabase(restoredRoot)
	if err != nil {
		t.Fatalf("initialize restored application database: %v", err)
	}
	defer reopened.Close()
	if err := reopened.QueryRow("SELECT value FROM app_state WHERE key=?", key).Scan(&got); err != nil || got != "restored-only" {
		t.Fatalf("restored data lost on application startup: %q, %v", got, err)
	}
	providersAfter, err := os.ReadFile(providersPath)
	if err != nil {
		t.Fatalf("re-read provider configuration: %v", err)
	}
	if string(providersAfter) != string(providersBefore) {
		t.Fatal("backup/restore modified the original provider configuration")
	}
	if _, err := os.Stat(filepath.Join(app.RootPath, "data", "planner.db")); err != nil {
		t.Fatalf("live database missing after isolated restore: %v", err)
	}
}
