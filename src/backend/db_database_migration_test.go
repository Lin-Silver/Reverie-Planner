package main

import (
    "database/sql"
    "testing"
)

func databaseMigrationVersion(t *testing.T, db *sql.DB) int {
    t.Helper()
    var count int
    if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=8").Scan(&count); err != nil { t.Fatal(err) }
    return count
}

func TestDatabaseMigrationEightIsIdempotentAndPreservesRows(t *testing.T) {
    ctx := setupTestContext(t)
    root := ctx.RootPath
    db, rec := dbRegressionCreate(t, ctx)
    if count := databaseMigrationVersion(t,ctx.DB); count != 1 { t.Fatalf("missing migration8: %d",count) }
    if _, err := ctx.DB.Exec("DELETE FROM schema_migrations WHERE version=8"); err != nil { t.Fatal(err) }
    if err := ctx.DB.Close(); err != nil { t.Fatal(err) }
    upgraded, err := initializeDatabase(root)
    if err != nil { t.Fatalf("existing database migration: %v",err) }
    if count := databaseMigrationVersion(t,upgraded); count != 1 { t.Fatalf("migration8 not recorded: %d",count) }
    if err := upgraded.Close(); err != nil { t.Fatal(err) }
    again, err := initializeDatabase(root)
    if err != nil { t.Fatalf("migration rerun: %v",err) }
    defer again.Close()
    var title string
    if err := again.QueryRow("SELECT title FROM db_records WHERE id=?",rec.ID).Scan(&title); err != nil { t.Fatal(err) }
    if title != rec.Title { t.Fatalf("lost record during migration %q != %q",title,rec.Title) }
    var count int
    if err := again.QueryRow("SELECT COUNT(*) FROM databases WHERE id=?",db.ID).Scan(&count); err != nil { t.Fatal(err) }
    if count != 1 { t.Fatal("database disappeared after migration") }
}

func TestDatabaseMigrationEightUpgradesPartiallyExistingColumns(t *testing.T) {
    ctx := setupTestContext(t)
    root := ctx.RootPath
    if _,err:=ctx.DB.Exec("DELETE FROM schema_migrations WHERE version=8");err!=nil {t.Fatal(err)}
    if _,err:=ctx.DB.Exec("ALTER TABLE pages DROP COLUMN linked_view_id");err!=nil {t.Fatalf("prepare legacy schema: %v",err)}
    if err:=ctx.DB.Close();err!=nil {t.Fatal(err)}
    db,err:=initializeDatabase(root)
    if err!=nil {t.Fatalf("partial legacy upgrade: %v",err)}
    defer db.Close()
    if count:=databaseMigrationVersion(t,db);count!=1 {t.Fatal("partial upgrade not recorded")}
    rows,err:=db.Query("PRAGMA table_info(pages)")
    if err!=nil {t.Fatal(err)}
    defer rows.Close()
    found:=map[string]bool{}
    for rows.Next(){
        var cid,notnull,pk int
        var name,kind string
        var defaultValue sql.NullString
        if err:=rows.Scan(&cid,&name,&kind,&notnull,&defaultValue,&pk);err!=nil {t.Fatal(err)}
        found[name]=true
    }
    if err:=rows.Err();err!=nil {t.Fatal(err)}
    if !found["linked_view_id"] || !found["linked_database_id"] {t.Fatalf("missing linked columns: %v",found)}
}
