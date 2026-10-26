package main

import (
 "context"
 "database/sql"
 "os"
 "path/filepath"
 "testing"

 _ "modernc.org/sqlite"
)

// A backup must be a readable SQLite snapshot, independent of the live database.
func TestDatabaseBackupSnapshotAndSourcePreservation(t *testing.T) {
 app := setupTestContext(t)
 defer app.DB.Close()
 const key = "backup-regression-marker"
 const original = "before-backup"
 if _, err := app.DB.Exec("INSERT INTO app_state (key, value) VALUES (?, ?)", key, original); err != nil {
  t.Fatalf("insert source marker: %v", err)
 }
 sourcePath := filepath.Join(app.RootPath, "data", "planner.db")
 sourceBefore, err := os.Stat(sourcePath)
 if err != nil { t.Fatalf("stat source: %v", err) }
 backupPath, err := createDatabaseBackup(context.Background(), app.DB, app.RootPath)
 if err != nil { t.Fatalf("create backup: %v", err) }
 if backupPath == sourcePath { t.Fatal("backup path equals live database") }
 if filepath.Dir(backupPath) != filepath.Join(app.RootPath, "data", "backups") { t.Fatalf("unexpected backup directory: %s", backupPath) }
 snapshot, err := sql.Open("sqlite", backupPath)
 if err != nil { t.Fatalf("open snapshot: %v", err) }
 defer snapshot.Close()
 var integrity string
 if err := snapshot.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
  t.Fatalf("backup integrity: %q, %v", integrity, err)
 }
 var got string
 if err := snapshot.QueryRow("SELECT value FROM app_state WHERE key=?", key).Scan(&got); err != nil || got != original {
  t.Fatalf("snapshot marker: %q, %v", got, err)
 }
 if _, err := app.DB.Exec("UPDATE app_state SET value=? WHERE key=?", "after-backup", key); err != nil {
  t.Fatalf("update live database: %v", err)
 }
 if err := snapshot.QueryRow("SELECT value FROM app_state WHERE key=?", key).Scan(&got); err != nil || got != original {
  t.Fatalf("snapshot changed with source: %q, %v", got, err)
 }
 if err := app.DB.QueryRow("SELECT value FROM app_state WHERE key=?", key).Scan(&got); err != nil || got != "after-backup" {
  t.Fatalf("live marker missing: %q, %v", got, err)
 }
 sourceAfter, err := os.Stat(sourcePath)
 if err != nil || sourceAfter.IsDir() || sourceBefore.IsDir() {
  t.Fatalf("source database missing or invalid: %v", err)
 }
}
