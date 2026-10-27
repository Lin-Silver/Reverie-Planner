package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
)

// A position read outside the INSERT transaction lets simultaneous property
// creators commit the same position, which breaks stable schema ordering.
func TestDBPropertyConcurrentCreationAllocatesUniquePositions(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    const creators = 24
    type result struct {
        status int
        body string
    }
    results := make(chan result, creators)
    start := make(chan struct{})
    var wg sync.WaitGroup
    for i := 0; i < creators; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            <-start
            payload := fmt.Sprintf(`{"databaseId":%q,"name":%q,"type":"text","config":"{}"}`, database.ID, fmt.Sprintf("Concurrent property %d", i))
            req := httptest.NewRequest(http.MethodPost, "/api/db/properties", strings.NewReader(payload))
            res := httptest.NewRecorder()
            app.handleDBProperties(res, req)
            results <- result{res.Code, res.Body.String()}
        }(i)
    }
    close(start)
    wg.Wait()
    close(results)

    positions := map[int]string{database.Properties[0].Position: database.Properties[0].ID}
    for result := range results {
        if result.status != http.StatusCreated {
            t.Fatalf("concurrent property create failed: %d %s", result.status, result.body)
        }
        var p DBProperty
        if err := json.Unmarshal([]byte(result.body), &p); err != nil { t.Fatal(err) }
        if p.DatabaseID != database.ID || p.Version != 1 {
            t.Fatalf("created property has wrong owner/version: %+v", p)
        }
        if previous, exists := positions[p.Position]; exists {
            t.Fatalf("duplicate property position %d: %s and %s", p.Position, previous, p.ID)
        }
        positions[p.Position] = p.ID
    }
    if len(positions) != creators+1 {
        t.Fatalf("expected %d unique positions, got %d", creators+1, len(positions))
    }
    for i := 0; i <= creators; i++ {
        if _, ok := positions[i]; !ok { t.Fatalf("missing position %d", i) }
    }
    var count int
    if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=? AND deleted_at IS NULL", database.ID).Scan(&count); err != nil || count != creators+1 {
        t.Fatalf("persisted property count=%d err=%v", count, err)
    }
    saved, err := app.getDBRecord(record.ID)
    if err != nil || saved.Title != record.Title || saved.Version != record.Version {
        t.Fatalf("concurrent property creates changed record: %+v %v", saved, err)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBPropertyCreateTransactionRollsBackFailedInsert(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    database, record := dbRegressionCreate(t, app)
    providerPath, providerBefore := viewIntegrityProviders(t, app)
    original := database.Properties[0]

    if _, err := app.DB.Exec(`CREATE TRIGGER fail_property_insert BEFORE INSERT ON db_properties
        BEGIN SELECT RAISE(ABORT, 'simulated property insertion failure'); END`); err != nil { t.Fatal(err) }
    payload := fmt.Sprintf(`{"databaseId":%q,"name":"Retry property","type":"text","config":"{}"}`, database.ID)
    failed := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", payload, app.handleDBProperties)
    if failed.Code != http.StatusInternalServerError {
        t.Fatalf("failed SQLite insert should be 500: %d %s", failed.Code, failed.Body.String())
    }
    var count, version, position int
    if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", database.ID).Scan(&count); err != nil || count != 1 {
        t.Fatalf("failed insert changed property count: %d %v", count, err)
    }
    if err := app.DB.QueryRow("SELECT version,position FROM db_properties WHERE id=?", original.ID).Scan(&version, &position); err != nil { t.Fatal(err) }
    if version != original.Version || position != original.Position {
        t.Fatalf("failed insert changed original property: version=%d position=%d", version, position)
    }
    saved, err := app.getDBRecord(record.ID)
    if err != nil || saved.Title != record.Title || saved.Version != record.Version {
        t.Fatalf("failed insert changed record: %+v %v", saved, err)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)

    if _, err := app.DB.Exec("DROP TRIGGER fail_property_insert"); err != nil { t.Fatal(err) }
    recovered := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", payload, app.handleDBProperties)
    if recovered.Code != http.StatusCreated {
        t.Fatalf("create after removing SQLite fault: %d %s", recovered.Code, recovered.Body.String())
    }
    var p DBProperty
    if err := json.Unmarshal(recovered.Body.Bytes(), &p); err != nil { t.Fatal(err) }
    if p.Position != 1 || p.Version != 1 {
        t.Fatalf("recovered property position/version incorrect: %+v", p)
    }
    assertViewIntegrityProviders(t, providerPath, providerBefore)
}
