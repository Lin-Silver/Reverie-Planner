package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// A successful INSERT followed by a failed response readback must not leave
// an unreadable property/view committed. The fault affects only test SQLite.
func TestDBSchemaCreateReadbackFailureRollsBack(t *testing.T) {
	cases := []struct {
		name, table, route, id, kind string
	}{
		{"property", "db_properties", "/api/db/properties", "readback_fault_property", "text"},
		{"view", "db_views", "/api/db/views", "readback_fault_view", "table"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupTestContext(t)
			defer app.DB.Close()
			database, record := dbRegressionCreate(t, app)
			providerPath, providerBefore := viewIntegrityProviders(t, app)

			var handler func(http.ResponseWriter, *http.Request)
			var payload string
			if tc.table == "db_properties" {
				handler = app.handleDBProperties
				payload = fmt.Sprintf(`{"id":%q,"databaseId":%q,"name":"Created after repair","type":"text","config":"{}"}`, tc.id, database.ID)
			} else {
				handler = app.handleDBViews
				payload = fmt.Sprintf(`{"id":%q,"databaseId":%q,"name":"Created after repair","type":"table","config":"{}"}`, tc.id, database.ID)
			}

			counts := map[string]int{}
			for _, table := range []string{"db_properties", "db_views", "db_records", "pages"} {
				var count int
				if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
					t.Fatal(err)
				}
				counts[table] = count
			}
			var originalPageTitle string
			var originalPageVersion int
			if err := app.DB.QueryRow("SELECT title, version FROM pages WHERE id=?", record.PageID).
				Scan(&originalPageTitle, &originalPageVersion); err != nil {
				t.Fatal(err)
			}

			// SQLite's INTEGER affinity allows a trigger to replace the version
			// with TEXT. The INSERT succeeds but scanning into int must fail.
			const trigger = "corrupt_schema_create_readback"
			if _, err := app.DB.Exec(fmt.Sprintf(`CREATE TRIGGER %s AFTER INSERT ON %s
				BEGIN UPDATE %s SET version='unscannable-version' WHERE id=NEW.id; END`,
				trigger, tc.table, tc.table)); err != nil {
				t.Fatal(err)
			}
			failed := dbRegressionCall(t, app, http.MethodPost, tc.route, payload, handler)
			if failed.Code != http.StatusInternalServerError {
				t.Fatalf("readback failure must be 500, got %d: %s", failed.Code, failed.Body.String())
			}
			if _, err := app.DB.Exec("DROP TRIGGER " + trigger); err != nil {
				t.Fatal(err)
			}

			for table, before := range counts {
				var after int
				if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if after != before {
					t.Fatalf("failed create changed %s row count: %d -> %d", table, before, after)
				}
			}
			var inserted int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + tc.table + " WHERE id=?", tc.id).Scan(&inserted); err != nil || inserted != 0 {
				t.Fatalf("failed create persisted an unreadable row: count=%d err=%v", inserted, err)
			}
			current, err := app.getDatabase(database.ID)
			if err != nil || current.Version != database.Version ||
				len(current.Properties) != len(database.Properties) || len(current.Views) != len(database.Views) ||
				current.Properties[0].Version != database.Properties[0].Version ||
				current.Views[0].Version != database.Views[0].Version {
				t.Fatalf("failed create changed existing schema: %+v err=%v", current, err)
			}
			saved, err := app.getDBRecord(record.ID)
			if err != nil || saved.Title != record.Title || saved.Version != record.Version {
				t.Fatalf("failed create changed record: %+v err=%v", saved, err)
			}
			var pageTitle string
			var pageVersion int
			if err := app.DB.QueryRow("SELECT title, version FROM pages WHERE id=?", record.PageID).
				Scan(&pageTitle, &pageVersion); err != nil {
				t.Fatal(err)
			}
			if pageTitle != originalPageTitle || pageVersion != originalPageVersion {
				t.Fatalf("failed create changed backing page: %q v%d", pageTitle, pageVersion)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)

			recovered := dbRegressionCall(t, app, http.MethodPost, tc.route, payload, handler)
			if recovered.Code != http.StatusCreated {
				t.Fatalf("create after fault removal: %d %s", recovered.Code, recovered.Body.String())
			}
			var returnedID, returnedName string
			var returnedVersion, returnedPosition int
			if tc.table == "db_properties" {
				var p DBProperty
				if err := json.Unmarshal(recovered.Body.Bytes(), &p); err != nil { t.Fatal(err) }
				if p.DatabaseID != database.ID || p.Type != tc.kind || !p.Visible {
					t.Fatalf("incomplete property response: %+v", p)
				}
				returnedID, returnedName, returnedVersion, returnedPosition = p.ID, p.Name, p.Version, p.Position
			} else {
				var v DBView
				if err := json.Unmarshal(recovered.Body.Bytes(), &v); err != nil { t.Fatal(err) }
				if v.DatabaseID != database.ID || v.Type != tc.kind {
					t.Fatalf("incomplete view response: %+v", v)
				}
				returnedID, returnedName, returnedVersion, returnedPosition = v.ID, v.Name, v.Version, v.Position
			}
			if returnedID != tc.id || returnedName != "Created after repair" ||
				returnedVersion != 1 || returnedPosition != 1 {
				t.Fatalf("unexpected creation response: id=%q name=%q version=%d position=%d",
					returnedID, returnedName, returnedVersion, returnedPosition)
			}
			var storedName string
			var storedVersion, storedPosition int
			if err := app.DB.QueryRow("SELECT name, version, position FROM " + tc.table + " WHERE id=?", tc.id).
				Scan(&storedName, &storedVersion, &storedPosition); err != nil {
				t.Fatal(err)
			}
			if storedName != returnedName || storedVersion != returnedVersion || storedPosition != returnedPosition {
				t.Fatalf("response does not match stored row: %q v%d position=%d", storedName, storedVersion, storedPosition)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}
}
