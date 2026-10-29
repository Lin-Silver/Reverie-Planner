package main

import (
	"database/sql"
	"fmt"
)

// ensureTaskColumns upgrades legacy task tables without hiding SQLite failures.
// Every column name and definition below is a fixed application constant.
func ensureTaskColumns(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin task schema upgrade: %w", err)
	}
	defer tx.Rollback()

	var kind string
	if err := tx.QueryRow("SELECT type FROM sqlite_master WHERE name = 'tasks'").Scan(&kind); err != nil {
		return fmt.Errorf("inspect tasks table: %w", err)
	}
	if kind != "table" {
		return fmt.Errorf("expected tasks table, found %q", kind)
	}

	rows, err := tx.Query("PRAGMA table_info(tasks)")
	if err != nil {
		return fmt.Errorf("inspect task columns: %w", err)
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("read task column: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("scan task columns: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close task columns: %w", err)
	}

	for _, column := range []struct {
		name, definition string
	}{
		{"state_id", "TEXT"},
		{"cycle_id", "TEXT"},
		{"module_id", "TEXT"},
		{"estimate", "TEXT"},
		{"parent_id", "TEXT"},
		{"sequence_no", "INTEGER DEFAULT 0"},
		{"labels_json", "TEXT"},
		{"deleted_at", "TEXT"},
	} {
		if columns[column.name] {
			continue
		}
		if _, err := tx.Exec("ALTER TABLE tasks ADD COLUMN " + column.name + " " + column.definition); err != nil {
			return fmt.Errorf("add tasks.%s: %w", column.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task schema upgrade: %w", err)
	}
	return nil
}
