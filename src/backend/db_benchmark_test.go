package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDatabaseRecordsBenchmark10k seeds 10,000 records and exercises the
// paginated read path plus the COUNT, logging reproducible timings. It is a
// smoke benchmark (generous bound) to catch gross regressions, not a fixed
// hardware-independent threshold.
func TestDatabaseRecordsBenchmark10k(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	database, _ := dbRegressionCreate(t, app)

	var baseline int
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_records WHERE database_id=?", database.ID).Scan(&baseline); err != nil { t.Fatal(err) }
	const n = 10000
	start := time.Now()
	tx, err := app.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO db_records (id, database_id, title) VALUES (?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("bench-%06d", i), database.ID, fmt.Sprintf("Record %d", i)); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	seedDur := time.Since(start)

	// Paginated read of 100 rows at offset 5000.
	reqStart := time.Now()
	res := httptest.NewRecorder()
	app.handleDBRecords(res, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/db/records?databaseId=%s&limit=100&offset=5000", database.ID), nil))
	if res.Code != http.StatusOK {
		t.Fatalf("paginated read: %d %s", res.Code, res.Body.String())
	}
	var page struct {
		Records []DBRecord `json:"records"`
		Total   int        `json:"total"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	readDur := time.Since(reqStart)
	if page.Total != baseline+n {
		t.Fatalf("total: got %d want %d", page.Total, baseline+n)
	}
	if len(page.Records) != 100 {
		t.Fatalf("page len: got %d want 100", len(page.Records))
	}

	t.Logf("10k benchmark: seed=%s paginated read(100@5000)=%s total=%d", seedDur, readDur, page.Total)
	// Generous smoke bound; a real regression (e.g., full scan per page) would blow past this.
	if readDur > 5*time.Second {
		t.Fatalf("paginated read too slow: %s", readDur)
	}
}