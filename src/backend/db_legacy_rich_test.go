package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// insertRichContent seeds nested pages, multiple projects/tasks, a database with
// properties, records, values, a view and a file row plus an on-disk attachment.
// It returns the identifiers the assertions need. All writes go through the
// disposable test database; no live or production workspace is touched.
func insertRichContent(t *testing.T, app *AppContext) (parentPage, childPage, database, record, property, view string) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}

	exec(`INSERT INTO projects(id,name,status) VALUES('rp1','Rich One','active')`)
	exec(`INSERT INTO projects(id,name,status) VALUES('rp2','Rich Two','planning')`)
	exec(`INSERT INTO tasks(id,project_id,title) VALUES('rt1','rp1','Rich task one')`)
	exec(`INSERT INTO tasks(id,project_id,title) VALUES('rt2','rp2','Rich task two')`)
	exec(`INSERT INTO app_state(key,value) VALUES('rich-marker','persisted')`)

	parentPage = "rich-page-parent"
	childPage = "rich-page-child"
	database = "rich-db"
	record = "rich-record"
	property = "rich-prop"
	view = "rich-view"

	exec(`INSERT INTO pages(id,project_id,title,content) VALUES(?,?,?,?)`,
		parentPage, "rp1", "Parent page", "parent BlockNote body")
	exec(`INSERT INTO pages(id,project_id,parent_id,title,content) VALUES(?,?,?,?,?)`,
		childPage, "rp1", parentPage, "Child page", "child BlockNote body")
	exec(`INSERT INTO pages(id,project_id,title) VALUES('rich-db-page','rp1','Database page')`)

	exec(`INSERT INTO databases(id,page_id,title) VALUES(?,?,?)`, database, "rich-db-page", "Rich database")
	exec(`INSERT INTO db_properties(id,database_id,name,type,config,position) VALUES(?,?,?,?,?,?)`,
		property, database, "Status", "select", `{"options":[{"id":"todo"}]}`, 0)
	exec(`INSERT INTO db_records(id,database_id,title) VALUES(?,?,?)`, record, database, "Rich record")
	exec(`INSERT INTO db_values(record_id,property_id,value) VALUES(?,?,?)`, record, property, `{"selected":"todo"}`)
	exec(`INSERT INTO db_views(id,database_id,name,type,config,position) VALUES(?,?,?,?,?,?)`,
		view, database, "Rich view", "table", `{}`, 0)

	attachment := filepath.Join(app.RootPath, "data", "rich-file.bin")
	if err := os.WriteFile(attachment, []byte{0, 1, 2, 3, 255}, 0o600); err != nil {
		t.Fatalf("write attachment: %v", err)
	}
	exec(`INSERT INTO db_files(id,database_id,record_id,original_name,content_type,size_bytes,sha256,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		"rich-file", database, record, "rich-file.bin", "application/octet-stream", 5, "deadbeef", "2026-01-01T00:00:00Z")

	return parentPage, childPage, database, record, property, view
}

// TestSyntheticV6RichUpgradePreservesNestedPagesAndContent proves a feature-rich
// pre-database (v6) workspace upgrades to the current schema without losing
// nested pages, multiple projects/tasks, AI state or provider configuration.
// The fixture is synthetic; real historical production backups still require
// owner-supplied acceptance assets.
func TestSyntheticV6RichUpgradePreservesNestedPagesAndContent(t *testing.T) {
	app := setupTestContext(t)
	root := app.RootPath
	insertRichContent(t, app)

	providerPath := filepath.Join(root, "config", "providers.json")
	before, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		"DROP TABLE db_files",
		"DROP TABLE db_values",
		"DROP TABLE db_views",
		"DROP TABLE db_properties",
		"DROP TABLE db_records",
		"DROP TABLE databases",
		"ALTER TABLE pages DROP COLUMN linked_view_id",
		"ALTER TABLE pages DROP COLUMN linked_database_id",
		"DELETE FROM schema_migrations WHERE version > 6",
	} {
		if _, err := app.DB.Exec(stmt); err != nil {
			t.Fatalf("construct synthetic v6 fixture %q: %v", stmt, err)
		}
	}
	if err := app.DB.Close(); err != nil {
		t.Fatal(err)
	}

	for restart := 0; restart < 2; restart++ {
		db, err := initializeDatabase(root)
		if err != nil {
			t.Fatalf("v6 rich upgrade restart %d: %v", restart, err)
		}
		for _, c := range []struct{ query, arg, want string }{
			{"SELECT title FROM pages WHERE id=?", "rich-page-parent", "Parent page"},
			{"SELECT content FROM pages WHERE id=?", "rich-page-parent", "parent BlockNote body"},
			{"SELECT title FROM pages WHERE id=?", "rich-page-child", "Child page"},
			{"SELECT parent_id FROM pages WHERE id=?", "rich-page-child", "rich-page-parent"},
			{"SELECT content FROM pages WHERE id=?", "rich-page-child", "child BlockNote body"},
			{"SELECT name FROM projects WHERE id=?", "rp2", "Rich Two"},
			{"SELECT title FROM tasks WHERE id=?", "rt2", "Rich task two"},
			{"SELECT value FROM app_state WHERE key=?", "rich-marker", "persisted"},
		} {
			var got string
			if err := db.QueryRow(c.query, c.arg).Scan(&got); err != nil || got != c.want {
				t.Fatalf("restart %d %s: got %q want %q err %v", restart, c.query, got, c.want, err)
			}
		}
		var migrations int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil || migrations != 10 {
			t.Fatalf("migration count %d: %v", migrations, err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}

	after, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("provider file changed: %v", err)
	}
}

// TestSyntheticV7RichUpgradePreservesDatabaseContentAndFiles proves a v7
// database with properties, records, values, a view and an attached file plus
// its on-disk bytes upgrades to the current schema intact and idempotently.
func TestSyntheticV7RichUpgradePreservesDatabaseContentAndFiles(t *testing.T) {
	app := setupTestContext(t)
	root := app.RootPath
	_, _, database, record, property, view := insertRichContent(t, app)

	providerPath := filepath.Join(root, "config", "providers.json")
	before, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	attachmentPath := filepath.Join(root, "data", "rich-file.bin")

	for _, stmt := range []string{
		"DROP TABLE db_files",
		"DROP INDEX idx_db_records_live",
		"DROP INDEX idx_db_properties_live",
		"DROP INDEX idx_db_views_live",
		"ALTER TABLE databases DROP COLUMN deleted_at",
		"ALTER TABLE db_records DROP COLUMN deleted_at",
		"ALTER TABLE db_properties DROP COLUMN deleted_at",
		"ALTER TABLE db_views DROP COLUMN deleted_at",
		"DROP INDEX idx_db_records_database_created",
		"DROP INDEX idx_db_values_property",
		"DROP INDEX idx_db_properties_order",
		"DROP INDEX idx_db_views_order",
		"ALTER TABLE pages DROP COLUMN linked_view_id",
		"ALTER TABLE pages DROP COLUMN linked_database_id",
		"DELETE FROM schema_migrations WHERE version > 7",
	} {
		if _, err := app.DB.Exec(stmt); err != nil {
			t.Fatalf("construct synthetic v7 fixture %q: %v", stmt, err)
		}
	}
	if err := app.DB.Close(); err != nil {
		t.Fatal(err)
	}

	for restart := 0; restart < 2; restart++ {
		upgraded, err := initializeDatabase(root)
		if err != nil {
			t.Fatalf("v7 rich upgrade restart %d: %v", restart, err)
		}
		for _, c := range []struct{ query, arg, want string }{
			{"SELECT title FROM databases WHERE id=?", database, "Rich database"},
			{"SELECT name FROM db_properties WHERE id=?", property, "Status"},
			{"SELECT type FROM db_properties WHERE id=?", property, "select"},
			{"SELECT title FROM db_records WHERE id=?", record, "Rich record"},
			{"SELECT value FROM db_values WHERE record_id=? AND property_id=?", record, `{"selected":"todo"}`},
			{"SELECT name FROM db_views WHERE id=?", view, "Rich view"},
			{"SELECT title FROM pages WHERE id=?", "rich-page-child", "Child page"},
		} {
			var got string
			// db_values needs two args; handle it separately below.
			if c.query == "SELECT value FROM db_values WHERE record_id=? AND property_id=?" {
				if err := upgraded.QueryRow(c.query, c.arg, property).Scan(&got); err != nil || got != c.want {
					t.Fatalf("restart %d db_values: got %q want %q err %v", restart, got, c.want, err)
				}
				continue
			}
			if err := upgraded.QueryRow(c.query, c.arg).Scan(&got); err != nil || got != c.want {
				t.Fatalf("restart %d %s: got %q want %q err %v", restart, c.query, got, c.want, err)
			}
		}
		var migrations int
		if err := upgraded.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil || migrations != 10 {
			t.Fatalf("migrations=%d err=%v", migrations, err)
		}
		if err := upgraded.Close(); err != nil {
			t.Fatal(err)
		}
	}

	after, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("provider configuration modified: %v", err)
	}
	file, err := os.ReadFile(attachmentPath)
	if err != nil || !bytes.Equal(file, []byte{0, 1, 2, 3, 255}) {
		t.Fatalf("external file modified: %v", err)
	}
}