package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Existing SQLite workspaces may still use DELETE journaling. Startup must
// switch them to durable WAL without discarding user pages or provider config.
func TestDatabaseStartupUpgradesJournalPolicyWithoutLosingUserData(t *testing.T) {
	app := setupTestContext(t)
	root := app.RootPath
	providerPath := filepath.Join(root, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	const pageID = "wal-existing-user-page"
	const pageContent = "Existing BlockNote content must survive the journal transition"
	if _, err := app.DB.Exec(
		`INSERT INTO pages (id, title, content, workspace, version) VALUES (?, ?, ?, 1, 1)`,
		pageID, "Existing user page", pageContent,
	); err != nil {
		t.Fatalf("create disposable user page: %v", err)
	}
	if err := app.DB.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "data", "planner.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var oldMode string
	if err := legacy.QueryRow(`PRAGMA journal_mode = DELETE`).Scan(&oldMode); err != nil {
		_ = legacy.Close()
		t.Fatalf("set legacy journal mode: %v", err)
	}
	if oldMode != "delete" {
		_ = legacy.Close()
		t.Fatalf("fixture did not enter DELETE mode: %q", oldMode)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := initializeDatabase(root)
	if err != nil {
		t.Fatalf("restart existing workspace: %v", err)
	}
	defer upgraded.Close()
	var journalMode string
	if err := upgraded.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil || journalMode != "wal" {
		t.Fatalf("expected WAL after restart: mode=%q err=%v", journalMode, err)
	}
	var synchronous, foreignKeys int
	if err := upgraded.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil || synchronous != 2 {
		t.Fatalf("expected FULL synchronous mode: value=%d err=%v", synchronous, err)
	}
	if err := upgraded.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign key enforcement lost: value=%d err=%v", foreignKeys, err)
	}
	var title, contentAfter string
	var version int
	if err := upgraded.QueryRow(
		`SELECT title, content, version FROM pages WHERE id=?`, pageID,
	).Scan(&title, &contentAfter, &version); err != nil {
		t.Fatalf("read existing page: %v", err)
	}
	if title != "Existing user page" || contentAfter != pageContent || version != 1 {
		t.Fatalf("journal transition modified page: title=%q content=%q version=%d", title, contentAfter, version)
	}
	providerAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, providerAfter) {
		t.Fatalf("journal transition modified AI provider configuration: %v", err)
	}
}
