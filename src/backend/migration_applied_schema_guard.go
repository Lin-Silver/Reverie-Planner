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
        {7, []struct{name string; columns []string}{
            {"databases", []string{"id", "page_id", "title", "version"}},
            {"db_properties", []string{"id", "database_id", "name", "type", "config", "position", "version"}},
            {"db_records", []string{"id", "database_id", "title", "page_id", "version"}},
            {"db_values", []string{"record_id", "property_id", "value", "version"}},
            {"db_views", []string{"id", "database_id", "name", "type", "config", "position", "version"}},
        }},
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
        if err := verifyAppliedMigrationIndexes(db, requirement.version); err != nil {
            return fmt.Errorf("migration %d is marked applied but its index schema is incomplete: %w; back up and inspect the database before repair", requirement.version, err)
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

// verifyAppliedMigrationIndexes checks that migration-created lookup indexes
// still point to the intended table and ordered columns. A missing or
// incorrectly repurposed index can silently degrade record and relation
// queries, and IF NOT EXISTS would not repair an index with the wrong shape.
// These names are fixed application constants, never user-provided SQL.
func verifyAppliedMigrationIndexes(db *sql.DB, version int) error {
    type indexSpec struct {
        name, table string
        columns []string
    }
    var indexes []indexSpec
    switch version {
    case 8:
        indexes = []indexSpec{
            {"idx_db_records_database_created", "db_records", []string{"database_id", "created_at"}},
            {"idx_db_values_property", "db_values", []string{"property_id"}},
            {"idx_db_properties_order", "db_properties", []string{"database_id", "position"}},
            {"idx_db_views_order", "db_views", []string{"database_id", "position"}},
        }
    case 9:
        indexes = []indexSpec{
            {"idx_db_records_live", "db_records", []string{"database_id", "deleted_at", "created_at"}},
            {"idx_db_properties_live", "db_properties", []string{"database_id", "deleted_at", "position"}},
            {"idx_db_views_live", "db_views", []string{"database_id", "deleted_at", "position"}},
        }
    case 10:
        indexes = []indexSpec{
            {"idx_db_files_record", "db_files", []string{"record_id", "deleted_at"}},
            {"idx_db_files_database", "db_files", []string{"database_id"}},
        }
    }
    for _, index := range indexes {
        var owner string
        err := db.QueryRow("SELECT tbl_name FROM sqlite_master WHERE type='index' AND name=?", index.name).Scan(&owner)
        if errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("missing index %s on %s", index.name, index.table)
        }
        if err != nil {
            return fmt.Errorf("inspect index %s: %w", index.name, err)
        }
        if owner != index.table {
            return fmt.Errorf("index %s belongs to %s instead of %s", index.name, owner, index.table)
        }
        // Matching names and columns alone are insufficient: a UNIQUE index
        // may reject valid user writes, while a partial index may omit records
        // required by lookup queries. The migration defines plain indexes.
        var isUnique, isPartial int
        err = db.QueryRow(`SELECT "unique", partial FROM pragma_index_list(?) WHERE name=?`,
            index.table, index.name).Scan(&isUnique, &isPartial)
        if errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("index %s is missing from %s index metadata", index.name, index.table)
        }
        if err != nil {
            return fmt.Errorf("inspect index %s flags: %w", index.name, err)
        }
        if isUnique != 0 || isPartial != 0 {
            return fmt.Errorf("index %s has unexpected unique=%d partial=%d flags", index.name, isUnique, isPartial)
        }
        rows, err := db.Query("PRAGMA index_info(" + index.name + ")")
        if err != nil {
            return fmt.Errorf("inspect columns of index %s: %w", index.name, err)
        }
        var actual []string
        for rows.Next() {
            var sequence, columnID int
            var name sql.NullString
            if err := rows.Scan(&sequence, &columnID, &name); err != nil {
                rows.Close()
                return fmt.Errorf("read index %s columns: %w", index.name, err)
            }
            if !name.Valid {
                rows.Close()
                return fmt.Errorf("index %s contains an expression rather than a named column", index.name)
            }
            actual = append(actual, name.String)
        }
        if err := rows.Err(); err != nil {
            rows.Close()
            return fmt.Errorf("scan index %s: %w", index.name, err)
        }
        if err := rows.Close(); err != nil {
            return fmt.Errorf("close index %s: %w", index.name, err)
        }
        if len(actual) != len(index.columns) {
            return fmt.Errorf("index %s has %d columns, expected %d", index.name, len(actual), len(index.columns))
        }
        for i, expected := range index.columns {
            if actual[i] != expected {
                return fmt.Errorf("index %s column %d is %s, expected %s", index.name, i, actual[i], expected)
            }
        }
    }
    return nil
}
