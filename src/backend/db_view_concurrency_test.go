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

// The database uses a single SQLite connection. Before the position lookup
// and INSERT shared a transaction, concurrent requests could each observe
// the same MAX(position) and commit duplicate positions.
func TestDBViewConcurrentCreationAllocatesUniquePositions(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	const creators = 24
	type result struct {
		status int
		body   string
	}
	results := make(chan result, creators)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < creators; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			payload := fmt.Sprintf(`{"databaseId":%q,"name":%q,"type":"table"}`, database.ID, fmt.Sprintf("Concurrent %d", i))
			req := httptest.NewRequest(http.MethodPost, "/api/db/views", strings.NewReader(payload))
			res := httptest.NewRecorder()
			app.handleDBViews(res, req)
			results <- result{res.Code, res.Body.String()}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	positions := map[int]string{database.Views[0].Position: database.Views[0].ID}
	for result := range results {
		if result.status != http.StatusCreated {
			t.Fatalf("concurrent view creation failed: %d %s", result.status, result.body)
		}
		var view DBView
		if err := json.Unmarshal([]byte(result.body), &view); err != nil {
			t.Fatal(err)
		}
		if view.DatabaseID != database.ID || view.Version != 1 {
			t.Fatalf("created view has wrong owner/version: %+v", view)
		}
		if previous, exists := positions[view.Position]; exists {
			t.Fatalf("duplicate view position %d: %s and %s", view.Position, previous, view.ID)
		}
		positions[view.Position] = view.ID
	}
	if len(positions) != creators+1 {
		t.Fatalf("expected %d distinct views, got %d", creators+1, len(positions))
	}
	for i := 0; i <= creators; i++ {
		if _, ok := positions[i]; !ok {
			t.Fatalf("view position %d missing after concurrent creation", i)
		}
	}
	var persisted int
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_views WHERE database_id=? AND deleted_at IS NULL", database.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != creators+1 {
		t.Fatalf("persisted view count mismatch: %d", persisted)
	}
	saved, err := app.getDBRecord(record.ID)
	if err != nil || saved.Title != record.Title || saved.Version != record.Version {
		t.Fatalf("concurrent view creation changed record: %+v %v", saved, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBViewCreateTransactionRollsBackFailedInsert(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	original := database.Views[0]

	if _, err := app.DB.Exec(`CREATE TRIGGER fail_view_insert BEFORE INSERT ON db_views
		BEGIN SELECT RAISE(ABORT, 'simulated view insertion failure'); END`); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"databaseId":%q,"name":"Retried view","type":"table"}`, database.ID)
	failed := dbRegressionCall(t, app, http.MethodPost, "/api/db/views", payload, app.handleDBViews)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("SQLite insert failure must be 500, got %d: %s", failed.Code, failed.Body.String())
	}
	if count := linkedViewTestCount(t, app, database.ID); count != 1 {
		t.Fatalf("failed insert created a view: %d", count)
	}
	var version, position int
	if err := app.DB.QueryRow("SELECT version,position FROM db_views WHERE id=?", original.ID).Scan(&version, &position); err != nil {
		t.Fatal(err)
	}
	if version != original.Version || position != original.Position {
		t.Fatalf("failed insert changed original view: version=%d position=%d", version, position)
	}
	saved, err := app.getDBRecord(record.ID)
	if err != nil || saved.Title != record.Title || saved.Version != record.Version {
		t.Fatalf("failed insert changed record: %+v %v", saved, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	if _, err := app.DB.Exec("DROP TRIGGER fail_view_insert"); err != nil {
		t.Fatal(err)
	}
	recovered := dbRegressionCall(t, app, http.MethodPost, "/api/db/views", payload, app.handleDBViews)
	if recovered.Code != http.StatusCreated {
		t.Fatalf("create after removing SQLite fault: %d %s", recovered.Code, recovered.Body.String())
	}
	var view DBView
	if err := json.Unmarshal(recovered.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Position != 1 || linkedViewTestCount(t, app, database.ID) != 2 {
		t.Fatalf("recovery failed to allocate next position: %+v", view)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
