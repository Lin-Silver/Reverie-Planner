package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

func dbRegressionCall(t *testing.T, ctx *AppContext, method, path, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
    t.Helper()
    w := httptest.NewRecorder()
    handler(w, httptest.NewRequest(method, path, strings.NewReader(body)))
    return w
}

func dbRegressionCreate(t *testing.T, ctx *AppContext) (Database, DBRecord) {
    t.Helper()
    result := dbRegressionCall(t, ctx, "POST", "/api/databases", `{"title":"Regression"}`, ctx.handleDatabases)
    if result.Code != 201 { t.Fatalf("create database: %d %s", result.Code, result.Body.String()) }
    var db Database
    if err := json.Unmarshal(result.Body.Bytes(), &db); err != nil { t.Fatal(err) }
    result = dbRegressionCall(t, ctx, "POST", "/api/db/records", `{"databaseId":"` + db.ID + `","title":"Original"}`, ctx.handleDBRecords)
    if result.Code != 201 { t.Fatalf("create record: %d %s", result.Code, result.Body.String()) }
    var rec DBRecord
    if err := json.Unmarshal(result.Body.Bytes(), &rec); err != nil { t.Fatal(err) }
    return db, rec
}

func TestDatabaseRejectsInvalidConfiguredSelectAtomically(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    db, rec := dbRegressionCreate(t, ctx)
    cfg, err := json.Marshal(map[string]any{"options": []string{"Todo", "Done"}})
    if err != nil { t.Fatal(err) }
    payload, err := json.Marshal(map[string]any{"databaseId": db.ID, "name": "Status", "type": "select", "config": string(cfg)})
    if err != nil { t.Fatal(err) }
    prop := dbRegressionCall(t, ctx, "POST", "/api/db/properties", string(payload), ctx.handleDBProperties)
    if prop.Code != 201 { t.Fatalf("property: %d %s", prop.Code, prop.Body.String()) }
    var p DBProperty
    if err := json.Unmarshal(prop.Body.Bytes(), &p); err != nil { t.Fatal(err) }
    update := `{"id":"` + rec.ID + `","version":1,"title":"Corrupted","values":{"` + p.ID + `":"Invalid"}}`
    outcome := dbRegressionCall(t, ctx, "PUT", "/api/db/records", update, ctx.handleDBRecords)
    if outcome.Code != 400 { t.Fatalf("expected 400, got %d: %s", outcome.Code, outcome.Body.String()) }
    actual, err := ctx.getDBRecord(rec.ID)
    if err != nil { t.Fatal(err) }
    if actual.Title != "Original" || actual.Version != 1 || len(actual.Values) != 0 { t.Fatalf("non-atomic failed update: %+v", actual) }
}

func TestDatabaseRecordUpdateRejectsMissingLinkedPageAtomically(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    _, rec := dbRegressionCreate(t, ctx)
    if _, err := ctx.DB.Exec(`UPDATE db_records SET page_id=NULL WHERE id=?`, rec.ID); err != nil { t.Fatal(err) }
    response := dbRegressionCall(t, ctx, "PUT", "/api/db/records", `{"id":"` + rec.ID + `","title":"Broken","version":1,"values":{}}`, ctx.handleDBRecords)
    if response.Code < 400 { t.Fatalf("unexpected success: %s", response.Body.String()) }
    got, err := ctx.getDBRecord(rec.ID)
    if err != nil { t.Fatal(err) }
    if got.Title != "Original" || got.Version != 1 { t.Fatalf("updated despite missing page: %+v", got) }
}

func TestDatabaseListWithRecordsTerminates(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    dbRegressionCreate(t, ctx)
    result := make(chan *httptest.ResponseRecorder, 1)
    go func() { result <- dbRegressionCall(t, ctx, "GET", "/api/databases", "", ctx.handleDatabases) }()
    select {
    case res := <-result:
        if res.Code != 200 { t.Fatalf("list status %d: %s", res.Code, res.Body.String()) }
        var databases []Database
        if err := json.Unmarshal(res.Body.Bytes(), &databases); err != nil { t.Fatal(err) }
        if len(databases) == 0 || len(databases[0].Properties) == 0 || len(databases[0].Views) == 0 { t.Fatalf("incomplete db list: %+v", databases) }
    case <-time.After(3 * time.Second):
        t.Fatal("database list blocked (possible nested SQLite connection deadlock)")
    }
}
