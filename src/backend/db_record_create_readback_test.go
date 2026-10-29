package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Failed response readback must not leave an invisible record or an orphaned
// backing page. Each trigger below exists only in a disposable test database.
func TestDBRecordCreateReadbackRollsBackRecordAndPage(t *testing.T) {
	cases := []struct {
		name string
		trigger string
	}{
		{
			name: "unreadable record version",
			trigger: `CREATE TRIGGER corrupt_record_creation AFTER INSERT ON db_records
				WHEN NEW.title='Faulty record'
				BEGIN UPDATE db_records SET version='not-an-integer' WHERE id=NEW.id; END`,
		},
		{
			name: "unreadable backing page version",
			trigger: `CREATE TRIGGER corrupt_record_creation AFTER INSERT ON pages
				WHEN NEW.title='Faulty record'
				BEGIN UPDATE pages SET version='not-an-integer' WHERE id=NEW.id; END`,
		},
		{
			name: "inconsistent backing page title",
			trigger: `CREATE TRIGGER corrupt_record_creation AFTER INSERT ON pages
				WHEN NEW.title='Faulty record'
				BEGIN UPDATE pages SET title='unexpected title' WHERE id=NEW.id; END`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupTestContext(t)
			defer app.DB.Close()
			database, original := dbRegressionCreate(t, app)
			providerPath, providerBefore := viewIntegrityProviders(t, app)

			var beforeRecords, beforePages int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_records").Scan(&beforeRecords); err != nil {
				t.Fatal(err)
			}
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM pages").Scan(&beforePages); err != nil {
				t.Fatal(err)
			}
			if _, err := app.DB.Exec(tc.trigger); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"databaseId":%q,"title":"Faulty record"}`, database.ID)
			failed := dbRegressionCall(t, app, http.MethodPost, "/api/db/records", body, app.handleDBRecords)
			if failed.Code != http.StatusInternalServerError {
				t.Fatalf("unreadable creation must return 500, got %d: %s", failed.Code, failed.Body.String())
			}

			var records, pages, leakedRecords, leakedPages int
			for _, check := range []struct {
				query string
				out *int
			}{
				{"SELECT COUNT(*) FROM db_records", &records},
				{"SELECT COUNT(*) FROM pages", &pages},
				{"SELECT COUNT(*) FROM db_records WHERE title='Faulty record'", &leakedRecords},
				{"SELECT COUNT(*) FROM pages WHERE title IN ('Faulty record','unexpected title')", &leakedPages},
			} {
				if err := app.DB.QueryRow(check.query).Scan(check.out); err != nil {
					t.Fatal(err)
				}
			}
			if records != beforeRecords || pages != beforePages || leakedRecords != 0 || leakedPages != 0 {
				t.Fatalf("failed creation persisted rows: records %d/%d pages %d/%d leaked=%d/%d",
					records, beforeRecords, pages, beforePages, leakedRecords, leakedPages)
			}
			saved, err := app.getDBRecord(original.ID)
			if err != nil || saved.Title != original.Title || saved.Version != original.Version {
				t.Fatalf("failed creation changed existing record: %+v %v", saved, err)
			}
			var originalPageTitle string
			var originalPageVersion int
			if err := app.DB.QueryRow("SELECT title, version FROM pages WHERE id=?", original.PageID).
				Scan(&originalPageTitle, &originalPageVersion); err != nil {
				t.Fatal(err)
			}
			if originalPageTitle != original.Title || originalPageVersion != 1 {
				t.Fatalf("failed creation changed original page: %q version=%d", originalPageTitle, originalPageVersion)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)

			if _, err := app.DB.Exec("DROP TRIGGER corrupt_record_creation"); err != nil {
				t.Fatal(err)
			}
			retried := dbRegressionCall(t, app, http.MethodPost, "/api/db/records", body, app.handleDBRecords)
			if retried.Code != http.StatusCreated {
				t.Fatalf("creation after fault removal: %d %s", retried.Code, retried.Body.String())
			}
			var created DBRecord
			if err := json.Unmarshal(retried.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			if created.ID == "" || created.PageID == "" || created.DatabaseID != database.ID ||
				created.Title != "Faulty record" || created.Version != 1 {
				t.Fatalf("incomplete created response: %+v", created)
			}
			var persistedTitle, pageTitle string
			var persistedVersion, pageVersion int
			if err := app.DB.QueryRow("SELECT title, version FROM db_records WHERE id=? AND page_id=?",
				created.ID, created.PageID).Scan(&persistedTitle, &persistedVersion); err != nil {
				t.Fatal(err)
			}
			if err := app.DB.QueryRow("SELECT title, version FROM pages WHERE id=?",
				created.PageID).Scan(&pageTitle, &pageVersion); err != nil {
				t.Fatal(err)
			}
			if persistedTitle != created.Title || pageTitle != created.Title ||
				persistedVersion != created.Version || pageVersion != 1 {
				t.Fatalf("response does not match persisted record/page: record=%q/%d page=%q/%d",
					persistedTitle, persistedVersion, pageTitle, pageVersion)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}
}
