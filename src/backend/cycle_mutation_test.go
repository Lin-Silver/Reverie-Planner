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
    "time"
)

func TestCycleMutationsRejectInvalidWritesAndPreservePartialUpdates(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()

    const projectID = "cycle-regression-project"
    if _, err := app.DB.Exec(`INSERT INTO projects (id, name, status, created_at) VALUES (?,?,?,?)`,
        projectID, "Keep project", "active", time.Now().UTC().Format(time.RFC3339)); err != nil {
        t.Fatal(err)
    }
    request := func(method, path, body string) *httptest.ResponseRecorder {
        t.Helper()
        res := httptest.NewRecorder()
        app.handleCycles(res, httptest.NewRequest(method, path, strings.NewReader(body)))
        return res
    }
    created := request(http.MethodPost, "/api/cycles",
        fmt.Sprintf(`{"projectId":%q,"name":"Keep cycle","description":"Keep description","startDate":"2026-10-01","endDate":"2026-10-31","status":"active"}`, projectID))
    if created.Code != http.StatusCreated {
        t.Fatalf("create cycle: %d %s", created.Code, created.Body.String())
    }
    var cycle Cycle
    if err := json.Unmarshal(created.Body.Bytes(), &cycle); err != nil || cycle.ID == "" {
        t.Fatalf("decode created cycle: %+v err=%v", cycle, err)
    }
    providerPath := filepath.Join(app.RootPath, "config", "providers.json")
    beforeProviders, err := os.ReadFile(providerPath)
    if err != nil { t.Fatal(err) }

    checkUnchanged := func(t *testing.T, expectedName string) {
        t.Helper()
        var id, project, name, description, start, end, status string
        err := app.DB.QueryRow(`SELECT id,project_id,name,description,start_date,end_date,status FROM cycles WHERE id=?`, cycle.ID).
            Scan(&id, &project, &name, &description, &start, &end, &status)
        if err != nil || id != cycle.ID || project != projectID || name != expectedName ||
            description != "Keep description" || start != "2026-10-01" || end != "2026-10-31" || status != "active" {
            t.Fatalf("cycle changed: id=%q project=%q name=%q desc=%q dates=%q/%q status=%q err=%v",
                id, project, name, description, start, end, status, err)
        }
        var count int
        if err := app.DB.QueryRow(`SELECT COUNT(*) FROM cycles`).Scan(&count); err != nil || count != 1 {
            t.Fatalf("cycle count=%d err=%v", count, err)
        }
        afterProviders, err := os.ReadFile(providerPath)
        if err != nil || !bytes.Equal(beforeProviders, afterProviders) {
            t.Fatalf("provider config changed: %v", err)
        }
    }
    invalid := []struct { name, method, path, body string; status int }{
        {"trailing object", http.MethodPost, "/api/cycles", `{"name":"Bad"}{"name":"Other"}`, 400},
        {"duplicate field", http.MethodPost, "/api/cycles", `{"name":"Bad","name":"Other"}`, 400},
        {"malformed", http.MethodPost, "/api/cycles", `{"name":`, 400},
        {"wrong type", http.MethodPost, "/api/cycles", `{"name":42}`, 400},
        {"null", http.MethodPost, "/api/cycles", `null`, 400},
        {"array", http.MethodPost, "/api/cycles", `[{"name":"Bad"}]`, 400},
        {"oversized", http.MethodPost, "/api/cycles", fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 4<<20)), 400},
        {"missing project", http.MethodPost, "/api/cycles", `{"name":"Bad"}`, 400},
        {"blank name", http.MethodPost, "/api/cycles", fmt.Sprintf(`{"projectId":%q,"name":"  "}`, projectID), 400},
        {"invalid date", http.MethodPost, "/api/cycles", fmt.Sprintf(`{"projectId":%q,"name":"Bad","startDate":"2026-02-30"}`, projectID), 400},
        {"duplicate id", http.MethodPost, "/api/cycles", fmt.Sprintf(`{"id":%q,"projectId":%q,"name":"Bad"}`, cycle.ID, projectID), 500},
        {"invalid foreign key", http.MethodPost, "/api/cycles", `{"projectId":"missing","name":"Bad"}`, 500},
        {"put trailing object", http.MethodPut, "/api/cycles", fmt.Sprintf(`{"id":%q,"name":"Bad"}{}`, cycle.ID), 400},
        {"put duplicate", http.MethodPut, "/api/cycles", fmt.Sprintf(`{"id":%q,"name":"Bad","name":"Other"}`, cycle.ID), 400},
        {"put blank name", http.MethodPut, "/api/cycles", fmt.Sprintf(`{"id":%q,"name":"  "}`, cycle.ID), 400},
        {"put invalid date", http.MethodPut, "/api/cycles", fmt.Sprintf(`{"id":%q,"endDate":"2026-13-01"}`, cycle.ID), 400},
        {"put project change", http.MethodPut, "/api/cycles", fmt.Sprintf(`{"id":%q,"projectId":"other"}`, cycle.ID), 400},
        {"put missing id", http.MethodPut, "/api/cycles", `{"name":"Bad"}`, 400},
        {"put unknown id", http.MethodPut, "/api/cycles", `{"id":"missing","name":"Bad"}`, 404},
        {"delete missing id", http.MethodDelete, "/api/cycles", "", 400},
        {"delete unknown id", http.MethodDelete, "/api/cycles?id=missing", "", 404},
    }
    for _, tc := range invalid {
        t.Run(tc.name, func(t *testing.T) {
            res := request(tc.method, tc.path, tc.body)
            if res.Code != tc.status {
                t.Fatalf("expected %d got %d: %s", tc.status, res.Code, res.Body.String())
            }
            checkUnchanged(t, "Keep cycle")
        })
    }
    updated := request(http.MethodPut, "/api/cycles", fmt.Sprintf(`{"id":%q,"name":"Renamed cycle"}`, cycle.ID))
    if updated.Code != http.StatusOK {
        t.Fatalf("partial update: %d %s", updated.Code, updated.Body.String())
    }
    checkUnchanged(t, "Renamed cycle")
    deleted := request(http.MethodDelete, "/api/cycles?id="+cycle.ID, "")
    if deleted.Code != http.StatusNoContent {
        t.Fatalf("delete cycle: %d %s", deleted.Code, deleted.Body.String())
    }
    var count int
    if err := app.DB.QueryRow(`SELECT COUNT(*) FROM cycles`).Scan(&count); err != nil || count != 0 {
        t.Fatalf("deleted cycle persisted: count=%d err=%v", count, err)
    }
    afterProviders, err := os.ReadFile(providerPath)
    if err != nil || !bytes.Equal(beforeProviders, afterProviders) {
        t.Fatalf("provider config changed after deletion: %v", err)
    }
}
