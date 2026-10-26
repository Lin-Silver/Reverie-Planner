package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every database mutation endpoint must reject JSON followed by a second
// object without committing the first object. A streaming Decode alone would
// accept the first JSON value and silently ignore the rest of the request.
func TestDatabaseMutationsRejectTrailingJSONWithoutChangingData(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	request := func(method, path, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(method, path, strings.NewReader(body)))
		return res
	}
	fixture := func(path, body string, handler http.HandlerFunc, output any) {
		t.Helper()
		res := request(http.MethodPost, path, body, handler)
		if res.Code != http.StatusCreated {
			t.Fatalf("fixture creation %s: status=%d body=%s", path, res.Code, res.Body.String())
		}
		if err := json.Unmarshal(res.Body.Bytes(), output); err != nil {
			t.Fatalf("decode fixture %s: %v", path, err)
		}
	}
	var database Database
	fixture("/api/databases", `{"title":"Preserved database"}`, app.handleDatabases, &database)
	if database.ID == "" || database.PageID == "" {
		t.Fatalf("invalid database fixture: %+v", database)
	}
	var property DBProperty
	fixture("/api/db/properties",
		fmt.Sprintf(`{"databaseId":%q,"name":"Preserved property","type":"text","config":"{}"}`, database.ID),
		app.handleDBProperties, &property)
	var record DBRecord
	fixture("/api/db/records",
		fmt.Sprintf(`{"databaseId":%q,"title":"Preserved record"}`, database.ID),
		app.handleDBRecords, &record)
	var view DBView
	fixture("/api/db/views",
		fmt.Sprintf(`{"databaseId":%q,"name":"Preserved view","type":"list","config":"{}"}`, database.ID),
		app.handleDBViews, &view)

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, method, url, body string
		handler http.HandlerFunc
	}{
		{"database create", http.MethodPost, "/api/databases", `{"title":"Unexpected database"}{"title":"another"}`, app.handleDatabases},
		{"database rename", http.MethodPut, "/api/databases",
			fmt.Sprintf(`{"id":%q,"version":%d,"title":"Unexpected title"}{"extra":true}`, database.ID, database.Version), app.handleDatabases},
		{"property create", http.MethodPost, "/api/db/properties",
			fmt.Sprintf(`{"databaseId":%q,"name":"Unexpected property","type":"text"}{"extra":true}`, database.ID), app.handleDBProperties},
		{"property update", http.MethodPut, "/api/db/properties",
			fmt.Sprintf(`{"id":%q,"databaseId":%q,"version":%d,"name":"Unexpected name","type":"text","config":"{}","visible":true}{"extra":true}`,
				property.ID, database.ID, property.Version), app.handleDBProperties},
		{"record create", http.MethodPost, "/api/db/records",
			fmt.Sprintf(`{"databaseId":%q,"title":"Unexpected record"}{"extra":true}`, database.ID), app.handleDBRecords},
		{"record update", http.MethodPut, "/api/db/records",
			fmt.Sprintf(`{"id":%q,"version":%d,"title":"Unexpected record title","values":{}}{"extra":true}`,
				record.ID, record.Version), app.handleDBRecords},
		{"view create", http.MethodPost, "/api/db/views",
			fmt.Sprintf(`{"databaseId":%q,"name":"Unexpected view","type":"list"}{"extra":true}`, database.ID), app.handleDBViews},
		{"view update", http.MethodPut, "/api/db/views",
			fmt.Sprintf(`{"id":%q,"databaseId":%q,"version":%d,"name":"Unexpected view name","type":"list","config":"{}"}{"extra":true}`,
				view.ID, database.ID, view.Version), app.handleDBViews},
		{"linked view create", http.MethodPost, "/api/db/linked-view",
			fmt.Sprintf(`{"databaseId":%q,"pageId":%q,"name":"Unexpected linked view"}{"extra":true}`,
				database.ID, database.PageID), app.handleDBLinkedView},
		{"large record create", http.MethodPost, "/api/db/records",
			fmt.Sprintf(`{"databaseId":%q,"title":%q}`, database.ID, strings.Repeat("a", 4<<20)), app.handleDBRecords},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.method, tc.url, tc.body, tc.handler)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400, got %d: %s", res.Code, res.Body.String())
			}
			for _, check := range []struct {
				table string
				want int
			}{
				{"databases", 1},
				{"db_properties", 2},
				{"db_records", 1},
				{"db_views", 2},
				{"pages", 2},
			} {
				var count int
				if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + check.table).Scan(&count); err != nil || count != check.want {
					t.Fatalf("%s row count after bad mutation: got=%d want=%d err=%v", check.table, count, check.want, err)
				}
			}
			for _, item := range []struct {
				table, id, title, titleColumn string
				version int
			}{
				{"databases", database.ID, database.Title, "title", database.Version},
				{"db_properties", property.ID, property.Name, "name", property.Version},
				{"db_records", record.ID, record.Title, "title", record.Version},
				{"db_views", view.ID, view.Name, "name", view.Version},
			} {
				var title string
				var version int
				query := fmt.Sprintf("SELECT %s,version FROM %s WHERE id=?", item.titleColumn, item.table)
				err := app.DB.QueryRow(query, item.id).Scan(&title, &version)
				if err != nil || title != item.title || version != item.version {
					t.Fatalf("%s changed on malformed request: title=%q version=%d err=%v", item.table, title, version, err)
				}
			}
			var linked int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM pages WHERE linked_view_id IS NOT NULL").Scan(&linked); err != nil || linked != 0 {
				t.Fatalf("unexpected linked-view page changed: count=%d err=%v", linked, err)
			}
		})
	}
	providerAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, providerAfter) {
		t.Fatalf("JSON mutation validation changed AI provider config: %v", err)
	}
}
