package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDatabaseRecordsPagination verifies limit/offset pagination returns a
// windowed page with a total count, validates parameters, and preserves the
// legacy bare-array response when no limit is supplied.
func TestDatabaseRecordsPagination(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	database, _ := dbRegressionCreate(t, app)
	// Add 4 more records (5 total) directly.
	for i := 0; i < 4; i++ {
		if _, err := app.DB.Exec(`INSERT INTO db_records (id, database_id, title) VALUES (?,?,?)`, fmt.Sprintf("seed-%d", i), database.ID, fmt.Sprintf("Seed %d", i)); err != nil {
			t.Fatal(err)
		}
	}

	get := func(query string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		app.handleDBRecords(res, httptest.NewRequest(http.MethodGet, "/api/db/records?"+query, nil))
		return res
	}

	// Legacy mode: bare array of all 5.
	res := get("databaseId=" + database.ID)
	if res.Code != http.StatusOK {
		t.Fatalf("legacy: %d %s", res.Code, res.Body.String())
	}
	var legacy []DBRecord
	if err := json.Unmarshal(res.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("legacy not an array: %v", err)
	}
	if len(legacy) != 5 {
		t.Fatalf("legacy len: %d", len(legacy))
	}

	// Paginated first page.
	res = get(fmt.Sprintf("databaseId=%s&limit=2&offset=0", database.ID))
	if res.Code != http.StatusOK {
		t.Fatalf("page1: %d %s", res.Code, res.Body.String())
	}
	var page struct {
		Records []DBRecord `json:"records"`
		Total   int        `json:"total"`
		Limit   int        `json:"limit"`
		Offset  int        `json:"offset"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || page.Limit != 2 || page.Offset != 0 || len(page.Records) != 2 {
		t.Fatalf("page1: total=%d limit=%d offset=%d len=%d", page.Total, page.Limit, page.Offset, len(page.Records))
	}

	// Second page window.
	res = get(fmt.Sprintf("databaseId=%s&limit=2&offset=2", database.ID))
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || page.Offset != 2 {
		t.Fatalf("page2: len=%d offset=%d", len(page.Records), page.Offset)
	}

	// Invalid params.
	if res := get(fmt.Sprintf("databaseId=%s&limit=0", database.ID)); res.Code != http.StatusBadRequest {
		t.Fatalf("limit=0: %d", res.Code)
	}
	if res := get(fmt.Sprintf("databaseId=%s&limit=2&offset=-1", database.ID)); res.Code != http.StatusBadRequest {
		t.Fatalf("offset=-1: %d", res.Code)
	}
}