package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func createRecordBatchTestDatabase(t *testing.T, ctx *AppContext) (Database, string) {
	t.Helper()
	created := dbRegressionCall(t, ctx, "POST", "/api/databases", `{"title":"Batch"}`, ctx.handleDatabases)
	if created.Code != http.StatusCreated {
		t.Fatalf("create database: %d %s", created.Code, created.Body.String())
	}
	var db Database
	if err := json.Unmarshal(created.Body.Bytes(), &db); err != nil {
		t.Fatal(err)
	}
	prop := dbRegressionCall(t, ctx, "POST", "/api/db/properties",
		`{"databaseId":"`+db.ID+`","name":"Score","type":"number"}`, ctx.handleDBProperties)
	if prop.Code != http.StatusCreated {
		t.Fatalf("create property: %d %s", prop.Code, prop.Body.String())
	}
	var property DBProperty
	if err := json.Unmarshal(prop.Body.Bytes(), &property); err != nil {
		t.Fatal(err)
	}
	return db, property.ID
}

func callRecordBatch(t *testing.T, ctx *AppContext, databaseID string, records []map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"databaseId": databaseID, "records": records})
	if err != nil {
		t.Fatal(err)
	}
	return dbRegressionCall(t, ctx, "POST", "/api/db/records/batch", string(body), ctx.handleDBRecordsBatch)
}

func testBatchRecord(title, propertyID string, score int) map[string]any {
	return map[string]any{"title": title, "values": map[string]any{propertyID: score}}
}

func TestDBRecordBatchCreatesRecordsAndPages(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	db, propertyID := createRecordBatchTestDatabase(t, ctx)
	records := []map[string]any{}
	for i := 0; i < 5; i++ {
		records = append(records, map[string]any{
			"title":  fmt.Sprintf("Record %d", i),
			"values": map[string]any{propertyID: i},
		})
	}
	result := callRecordBatch(t, ctx, db.ID, records)
	if result.Code != http.StatusCreated {
		t.Fatalf("batch create: %d %s", result.Code, result.Body.String())
	}
	var created struct {
		Created int      `json:"created"`
		IDs     []string `json:"ids"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Created != len(records) || len(created.IDs) != len(records) {
		t.Fatalf("created count = %d ids = %d", created.Created, len(created.IDs))
	}
	list := dbRegressionCall(t, ctx, "GET", "/api/db/records?databaseId="+db.ID, "", ctx.handleDBRecords)
	if list.Code != http.StatusOK {
		t.Fatalf("list records: %d %s", list.Code, list.Body.String())
	}
	var stored []DBRecord
	if err := json.Unmarshal(list.Body.Bytes(), &stored); err != nil {
		t.Fatalf("legacy response must remain a JSON array: %v", err)
	}
	if len(stored) != len(records) {
		t.Fatalf("stored %d records, want %d", len(stored), len(records))
	}
	for i, rec := range stored {
		if rec.ID != created.IDs[i] {
			t.Fatalf("record %d id = %s, want %s", i, rec.ID, created.IDs[i])
		}
		if rec.PageID == "" {
			t.Fatalf("record %d has no backing page", i)
		}
		var pageTitle string
		if err := ctx.DB.QueryRow(`SELECT title FROM pages WHERE id=?`, rec.PageID).Scan(&pageTitle); err != nil {
			t.Fatalf("read backing page: %v", err)
		}
		if pageTitle != rec.Title {
			t.Fatalf("page title %q != record title %q", pageTitle, rec.Title)
		}
		if got, ok := rec.Values[propertyID].(float64); !ok || int(got) != i {
			t.Fatalf("record %d score = %#v, want %d", i, rec.Values[propertyID], i)
		}
	}
}

func TestDBRecordBatchRollsBackUnknownProperty(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	db, propertyID := createRecordBatchTestDatabase(t, ctx)
	valid := map[string]any{"title": "Valid", "values": map[string]any{propertyID: 1}}
	invalid := map[string]any{"title": "Invalid", "values": map[string]any{"prop_missing": 2}}
	result := callRecordBatch(t, ctx, db.ID, []map[string]any{valid, invalid})
	if result.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", result.Code, result.Body.String())
	}
	records, pages := countBatchRows(t, ctx, db.ID)
	if records != 0 || pages != 0 {
		t.Fatalf("rollback left %d records and %d pages", records, pages)
	}
}

func TestDBRecordBatchRollsBackDuplicateID(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	db, propertyID := createRecordBatchTestDatabase(t, ctx)
	dup := "rec_fixed"
	values := func(score int) map[string]any {
		return map[string]any{"id": dup, "title": "Dup", "values": map[string]any{propertyID: score}}
	}
	result := callRecordBatch(t, ctx, db.ID, []map[string]any{values(1), values(2)})
	if result.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", result.Code, result.Body.String())
	}
	records, pages := countBatchRows(t, ctx, db.ID)
	if records != 0 || pages != 0 {
		t.Fatalf("rollback left %d records and %d pages", records, pages)
	}
}

func TestDBRecordBatchRejectsOversizedBatch(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	db, propertyID := createRecordBatchTestDatabase(t, ctx)
	records := make([]map[string]any, maxDBRecordBatch+1)
	for i := range records {
		records[i] = testBatchRecord("Too many", propertyID, i)
	}
	result := callRecordBatch(t, ctx, db.ID, records)
	if result.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", result.Code)
	}
	if records, _ := countBatchRows(t, ctx, db.ID); records != 0 {
		t.Fatalf("oversized batch stored %d records", records)
	}
}

func countBatchRows(t *testing.T, ctx *AppContext, databaseID string) (int, int) {
	t.Helper()
	records := 0
	pages := 0
	if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM db_records WHERE database_id=? AND deleted_at IS NULL`, databaseID).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM pages p JOIN db_records r ON r.page_id=p.id
		WHERE r.database_id=? AND r.deleted_at IS NULL`, databaseID).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	return records, pages
}

func TestDBRecordBatchRejectsMissingDatabase(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	result := callRecordBatch(t, ctx, "db_missing", []map[string]any{testBatchRecord("Nowhere", "prop_missing", 1)})
	if result.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", result.Code, result.Body.String())
	}
}

func TestDBRecordBatchRejectsUnknownMethod(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	request := httptest.NewRequest(http.MethodGet, "/api/db/records/batch", strings.NewReader(""))
	recorder := httptest.NewRecorder()
	ctx.handleDBRecordsBatch(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", recorder.Code)
	}
}
