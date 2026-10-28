package main

import (
    "database/sql"
    "errors"
    "fmt"
)

// verifyAppliedMigrationSchema rejects partially upgraded or damaged databases
// before startup can apply further migrations. Never repair an applied migration
// implicitly: its marker may coexist with user data needing manual recovery.
func verifyAppliedMigrationSchema(db *sql.DB) error {
    requirements := []struct {
        version int
        tables []struct {
            name string
            columns []string
        }
    }{
        {8, []struct{name string; columns []string}{
            {"pages", []string{"linked_database_id", "linked_view_id"}},
        }},
        {9, []struct{name string; columns []string}{
            {"databases", []string{"deleted_at"}},
            {"db_records", []string{"deleted_at"}},
            {"db_properties", []string{"deleted_at"}},
            {"db_views", []string{"deleted_at"}},
        }},
        {10, []struct{name string; columns []string}{
            {"db_files", []string{"id", "database_id", "record_id", "original_name", "content_type", "size_bytes", "sha256", "created_at", "deleted_at"}},
        }},
    }
    for _, requirement := range requirements {
        var applied int
        if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", requirement.version).Scan(&applied); err != nil {
            return fmt.Errorf("inspect migration %d: %w", requirement.version, err)
        }
        if applied == 0 {
            continue
        }
        for _, table := range requirement.tables {
            if err := verifyMigrationTableColumns(db, table.name, table.columns); err != nil {
                return fmt.Errorf("migration %d is marked applied but its schema is incomplete: %w; back up and inspect the database before repair", requirement.version, err)
            }
        }
    }
    return nil
}

// All table identifiers supplied by verifyAppliedMigrationSchema are fixed
// application constants, never user input.
func verifyMigrationTableColumns(db *sql.DB, table string, required []string) error {
    var kind string
    if err := db.QueryRow("SELECT type FROM sqlite_master WHERE name=?", table).Scan(&kind); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("missing table %s", table)
        }
        return fmt.Errorf("inspect table %s: %w", table, err)
    }
    if kind != "table" {
        return fmt.Errorf("expected table %s, found %s", table, kind)
    }
    rows, err := db.Query("PRAGMA table_info(" + table + ")")
    if err != nil {
        return fmt.Errorf("inspect columns of %s: %w", table, err)
    }
    found := make(map[string]bool)
    for rows.Next() {
        var cid, notNull, primaryKey int
        var name, dataType string
        var defaultValue sql.NullString
        if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
            rows.Close()
            return fmt.Errorf("read columns of %s: %w", table, err)
        }
        found[name] = true
    }
    if err := rows.Err(); err != nil {
        rows.Close()
        return fmt.Errorf("scan columns of %s: %w", table, err)
    }
    if err := rows.Close(); err != nil {
        return fmt.Errorf("close columns of %s: %w", table, err)
    }
    for _, column := range required {
        if !found[column] {
            return fmt.Errorf("missing %s.%s", table, column)
        }
    }
    return nil
}
