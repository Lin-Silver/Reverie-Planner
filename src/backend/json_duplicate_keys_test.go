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

func TestRejectDuplicateJSONKeysNestedAndEscaped(t *testing.T) {
    cases := []struct {
        name, body string
        reject bool
    }{
        {"top level duplicate", `{"title":"first","title":"last"}`, true},
        {"escaped equivalent key", `{"title":"first","\\u0074itle":"last"}`, true},
        {"nested duplicate", `{"values":{"item":{"id":1,"id":2}}}`, true},
        {"duplicate inside array", `{"values":[{"id":1,"id":2}]}`, true},
        {"same key in distinct objects", `{"values":[{"id":1},{"id":2}]}`, false},
        {"same key at different depths", `{"title":"outer","values":{"title":"inner"}}`, false},
        {"mixed valid scalars", `{"values":[null,true,12.5,"text",{"id":"x"}]}`, false},
        {"trailing object", `{"title":"first"}{"title":"second"}`, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            err := rejectDuplicateJSONKeys([]byte(tc.body))
            if (err != nil) != tc.reject {
                t.Fatalf("reject=%v err=%v for %s", tc.reject, err, tc.body)
            }
        })
    }
}

func TestDuplicateJSONMutationsPreserveDataAndProviderConfig(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()

    request := func(method, path, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
        t.Helper()
        res := httptest.NewRecorder()
        handler.ServeHTTP(res, httptest.NewRequest(method, path, strings.NewReader(body)))
        return res
    }

    created := request(http.MethodPost, "/api/databases", `{"title":"Keep database"}`, app.handleDatabases)
    if created.Code != http.StatusCreated {
        t.Fatalf("create database: %d %s", created.Code, created.Body.String())
    }
    var database Database
    if err := json.Unmarshal(created.Body.Bytes(), &database); err != nil {
        t.Fatal(err)
    }
    created = request(http.MethodPost, "/api/db/records",
        fmt.Sprintf(`{"databaseId":%q,"title":"Keep record"}`, database.ID), app.handleDBRecords)
    if created.Code != http.StatusCreated {
        t.Fatalf("create record: %d %s", created.Code, created.Body.String())
    }
    var record DBRecord
    if err := json.Unmarshal(created.Body.Bytes(), &record); err != nil {
        t.Fatal(err)
    }

    providerPath := filepath.Join(app.RootPath, "config", "providers.json")
    providerBefore, err := os.ReadFile(providerPath)
    if err != nil {
        t.Fatal(err)
    }
    counts := make(map[string]int)
    for _, table := range []string{"databases", "db_records", "pages", "projects"} {
        if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&counts[table]); err != nil {
            t.Fatal(err)
        }
    }

    cases := []struct {
        name, method, path, body string
        handler http.HandlerFunc
    }{
        {"database create duplicate title", http.MethodPost, "/api/databases",
            `{"title":"Unexpected","title":"Other"}`, app.handleDatabases},
        {"record create duplicate title", http.MethodPost, "/api/db/records",
            fmt.Sprintf(`{"databaseId":%q,"title":"Unexpected","title":"Other"}`, database.ID), app.handleDBRecords},
        {"record update duplicate title", http.MethodPut, "/api/db/records",
            fmt.Sprintf(`{"id":%q,"version":%d,"title":"Unexpected","title":"Other"}`, record.ID, record.Version), app.handleDBRecords},
        {"record update nested duplicate", http.MethodPut, "/api/db/records",
            fmt.Sprintf(`{"id":%q,"version":%d,"values":{"nested":{"id":1,"id":2}}}`, record.ID, record.Version), app.handleDBRecords},
        {"page create duplicate title", http.MethodPost, "/api/pages",
            `{"title":"Unexpected","title":"Other"}`, app.handlePages},
        {"project create duplicate name", http.MethodPost, "/api/projects",
            `{"name":"Unexpected","name":"Other"}`, app.handleProjects},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            res := request(tc.method, tc.path, tc.body, tc.handler)
            if res.Code != http.StatusBadRequest {
                t.Fatalf("expected HTTP 400, got %d: %s", res.Code, res.Body.String())
            }
            for table, want := range counts {
                var count int
                if err := app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
                    t.Fatalf("%s count changed: got=%d want=%d err=%v", table, count, want, err)
                }
            }
            var title string
            var version int
            if err := app.DB.QueryRow("SELECT title, version FROM db_records WHERE id=?", record.ID).Scan(&title, &version); err != nil ||
                title != record.Title || version != record.Version {
                t.Fatalf("record changed: title=%q version=%d err=%v", title, version, err)
            }
            providerAfter, err := os.ReadFile(providerPath)
            if err != nil || !bytes.Equal(providerBefore, providerAfter) {
                t.Fatalf("provider config changed: %v", err)
            }
        })
    }

    // Strict duplicate detection must not reject a normal optimistic update.
    res := request(http.MethodPut, "/api/db/records",
        fmt.Sprintf(`{"id":%q,"version":%d,"title":"Saved record","values":{}}`, record.ID, record.Version),
        app.handleDBRecords)
    if res.Code != http.StatusOK {
        t.Fatalf("valid record edit failed: %d %s", res.Code, res.Body.String())
    }
    var title string
    var version int
    if err := app.DB.QueryRow("SELECT title, version FROM db_records WHERE id=?", record.ID).Scan(&title, &version); err != nil ||
        title != "Saved record" || version != record.Version+1 {
        t.Fatalf("valid record edit not persisted: title=%q version=%d err=%v", title, version, err)
    }
}
