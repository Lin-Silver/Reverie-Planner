package main

import (
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "testing"
)

// A failed response readback must never commit a newly created database with
// unreadable default schema or leave its backing page orphaned.
func TestDatabaseCreateReadbackFailureRollsBackAllRows(t *testing.T) {
    cases := []struct {
        name, table string
    }{
        {"default property readback", "db_properties"},
        {"default view readback", "db_views"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            app := setupTestContext(t)
            defer app.DB.Close()
            existing, record := dbRegressionCreate(t, app)
            providerPath, providerBefore := viewIntegrityProviders(t, app)

            tables := []string{"databases", "db_properties", "db_views", "db_records", "pages"}
            before := make(map[string]int, len(tables))
            for _, table := range tables {
                var count int
                if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
                    t.Fatal(err)
                }
                before[table] = count
            }
            var originalPageTitle string
            var originalPageVersion int
            if err := app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?", existing.PageID).
                Scan(&originalPageTitle, &originalPageVersion); err != nil {
                t.Fatal(err)
            }

            trigger := "corrupt_database_creation_readback"
            if _, err := app.DB.Exec(fmt.Sprintf(`CREATE TRIGGER %s AFTER INSERT ON %s
                BEGIN UPDATE %s SET version='unscannable-version' WHERE id=NEW.id; END`,
                trigger, tc.table, tc.table)); err != nil {
                t.Fatal(err)
            }

            const newID = "db_create_readback_fault"
            payload := `{"id":"` + newID + `","title":"Should not persist"}`
            failed := dbRegressionCall(t, app, http.MethodPost, "/api/databases", payload, app.handleDatabases)
            if failed.Code != http.StatusInternalServerError {
                t.Fatalf("unreadable schema must fail with 500: %d %s", failed.Code, failed.Body.String())
            }
            if _, err := app.DB.Exec("DROP TRIGGER " + trigger); err != nil {
                t.Fatal(err)
            }

            for _, table := range tables {
                var after int
                if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&after); err != nil {
                    t.Fatal(err)
                }
                if after != before[table] {
                    t.Fatalf("failed create changed %s rows: %d -> %d", table, before[table], after)
                }
            }
            if _, err := app.getDatabase(newID); !errors.Is(err, sql.ErrNoRows) {
                t.Fatalf("failed create left a database: %v", err)
            }
            saved, err := app.getDBRecord(record.ID)
            if err != nil || saved.Title != record.Title || saved.Version != record.Version {
                t.Fatalf("failed create changed existing record: %+v %v", saved, err)
            }
            var pageTitle string
            var pageVersion int
            if err := app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?", existing.PageID).
                Scan(&pageTitle, &pageVersion); err != nil {
                t.Fatal(err)
            }
            if pageTitle != originalPageTitle || pageVersion != originalPageVersion {
                t.Fatalf("failed create changed existing page: %q v%d", pageTitle, pageVersion)
            }
            assertViewIntegrityProviders(t, providerPath, providerBefore)

            recovered := dbRegressionCall(t, app, http.MethodPost, "/api/databases", payload, app.handleDatabases)
            if recovered.Code != http.StatusCreated {
                t.Fatalf("create after fault removal: %d %s", recovered.Code, recovered.Body.String())
            }
            var created Database
            if err := json.Unmarshal(recovered.Body.Bytes(), &created); err != nil {
                t.Fatal(err)
            }
            if created.ID != newID || created.Title != "Should not persist" ||
                created.PageID == "" || created.Version != 1 ||
                len(created.Properties) != 1 || created.Properties[0].Type != "title" ||
                len(created.Views) != 1 || created.Views[0].Type != "table" {
                t.Fatalf("incomplete database creation response: %+v", created)
            }
            persisted, err := app.getDatabase(newID)
            if err != nil || persisted.PageID != created.PageID ||
                len(persisted.Properties) != 1 || len(persisted.Views) != 1 {
                t.Fatalf("created database not persisted with schema: %+v %v", persisted, err)
            }
            for _, table := range tables {
                var after int
                if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&after); err != nil {
                    t.Fatal(err)
                }
                expected := before[table]
                if table == "databases" || table == "db_properties" || table == "db_views" || table == "pages" {
                    expected++
                }
                if after != expected {
                    t.Fatalf("successful create changed %s rows unexpectedly: want %d got %d", table, expected, after)
                }
            }
            assertViewIntegrityProviders(t, providerPath, providerBefore)
        })
    }
}
