package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const bulkRecordCount = 10000

func tableConcurrentCall(a *AppContext, method, path, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	res := httptest.NewRecorder()
	handler(res, httptest.NewRequest(method, path, strings.NewReader(body)))
	return res
}

// bulkSeedRecords writes a large table through one transaction. Backing pages
// and property values are part of the same commit so a partial table can never
// be observed after a failure.
func bulkSeedRecords(t *testing.T, a *AppContext, databaseID, notesID, amountID string, total int) {
	t.Helper()
	tx, err := a.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := timeNowUTC()
	for i := 0; i < total; i++ {
		recordID := fmt.Sprintf("bulk_rec_%05d", i)
		pageID := fmt.Sprintf("bulk_page_%05d", i)
		title := fmt.Sprintf("Bulk row %05d", i)
		if _, err = tx.Exec(`INSERT INTO pages(id,title,content,workspace,created_by,version,created_at,updated_at) VALUES(?,?,'',1,'Bulk',1,?,?)`, pageID, title, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO db_records(id,database_id,title,page_id,version,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`, recordID, databaseID, title, pageID, now, now); err != nil {
			t.Fatal(err)
		}
		textJSON, _ := json.Marshal(fmt.Sprintf("note %05d", i))
		if _, err = tx.Exec(`INSERT INTO db_values(record_id,property_id,value,version,updated_at) VALUES(?,?,?,1,?)`, recordID, notesID, string(textJSON), now); err != nil {
			t.Fatal(err)
		}
		numberJSON, _ := json.Marshal(float64(i) / 100)
		if _, err = tx.Exec(`INSERT INTO db_values(record_id,property_id,value,version,updated_at) VALUES(?,?,?,1,?)`, recordID, amountID, string(numberJSON), now); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func readAllRecords(t *testing.T, a *AppContext, databaseID string) []DBRecord {
	t.Helper()
	res := tableConcurrentCall(a, "GET", "/api/db/records?databaseId="+databaseID, "", a.handleDBRecords)
	if res.Code != 200 {
		t.Fatalf("list records: %d %s", res.Code, res.Body.String())
	}
	var records []DBRecord
	if err := json.Unmarshal(res.Body.Bytes(), &records); err != nil {
		t.Fatalf("decode records: %v", err)
	}
	return records
}

func assertBulkTable(t *testing.T, a *AppContext, databaseID, notesID, amountID, doubleID string) {
	t.Helper()
	records := readAllRecords(t, a, databaseID)
	if len(records) != bulkRecordCount+1 {
		t.Fatalf("record count %d", len(records))
	}
	seen := map[string]bool{}
	byTitle := map[string]DBRecord{}
	for _, rec := range records {
		if rec.ID == "" || seen[rec.ID] {
			t.Fatalf("duplicate or empty record id: %q", rec.ID)
		}
		seen[rec.ID] = true
		if rec.PageID == "" || rec.Version < 1 {
			t.Fatalf("record lost backing page/version: %+v", rec)
		}
		byTitle[rec.Title] = rec
	}
	for i := 0; i < bulkRecordCount; i++ {
		title := fmt.Sprintf("Bulk row %05d", i)
		rec, ok := byTitle[title]
		if !ok {
			t.Fatalf("row %d missing", i)
		}
		amount := float64(i) / 100
		if rec.Values[notesID] != fmt.Sprintf("note %05d", i) || rec.Values[amountID] != amount || rec.Values[doubleID] != amount*2 {
			t.Fatalf("row %d values: %+v", i, rec.Values)
		}
	}
}

func assertSampledBulkRows(t *testing.T, a *AppContext, databaseID, notesID, amountID, doubleID string) {
	t.Helper()
	records := readAllRecords(t, a, databaseID)
	if len(records) != bulkRecordCount {
		t.Fatalf("record count after delete: %d", len(records))
	}
	byTitle := map[string]DBRecord{}
	for _, rec := range records {
		byTitle[rec.Title] = rec
		if rec.PageID == "" || rec.Version < 1 {
			t.Fatalf("record lost backing page/version after restart: %+v", rec)
		}
	}
	if _, ok := byTitle["Bulk row 00777"]; ok {
		t.Fatal("deleted row came back after restart")
	}
	for _, i := range []int{0, 98, 1000, 5000, 9999} {
		rec, ok := byTitle[fmt.Sprintf("Bulk row %05d", i)]
		if !ok {
			t.Fatalf("row %d missing after restart", i)
		}
		if i < 99 {
			if rec.Values[amountID] != 4242.5 || rec.Values[doubleID] != 8485.0 {
				t.Fatalf("bulk edit not durable for row %d: %+v", i, rec.Values)
			}
			continue
		}
		amount := float64(i) / 100
		if rec.Values[notesID] != fmt.Sprintf("note %05d", i) || rec.Values[amountID] != amount || rec.Values[doubleID] != amount*2 {
			t.Fatalf("row %d changed during restart: %+v", i, rec.Values)
		}
	}
}

func TestLargeTableReadWriteAndRestartKeepsEveryRecord(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	var err error
	database, _ := dbRegressionCreate(t, a)
	notes := computedProperty(t, a, database.ID, "Notes", "text", map[string]any{})
	amount := computedProperty(t, a, database.ID, "Amount", "number", map[string]any{})
	formula := computedProperty(t, a, database.ID, "Double", "formula", map[string]any{"operation": "multiply", "args": []any{map[string]any{"propertyId": amount.ID}, map[string]any{"number": 2}}})
	bulkSeedRecords(t, a, database.ID, notes.ID, amount.ID, bulkRecordCount)
	assertBulkTable(t, a, database.ID, notes.ID, amount.ID, formula.ID)

	// A batch update is all-or-nothing: one stale version must leave every other
	// row untouched instead of saving a partial batch.
	var edits []map[string]any
	for i := 0; i < 99; i++ {
		edits = append(edits, map[string]any{
			"id":      fmt.Sprintf("bulk_rec_%05d", i),
			"version": 1,
			"values":  map[string]any{amount.ID: 4242.5},
		})
	}
	conflicting := append(append([]map[string]any{}, edits...), map[string]any{
		"id":      fmt.Sprintf("bulk_rec_%05d", bulkRecordCount-1),
		"version": 99,
		"values":  map[string]any{amount.ID: 7.0},
	})
	payload, _ := json.Marshal(map[string]any{"databaseId": database.ID, "edits": conflicting})
	res := tableConcurrentCall(a, "POST", "/api/db/bulk-edit", string(payload), a.handleDBBulkEdit)
	if res.Code != 409 {
		t.Fatalf("stale batch was accepted: %d %s", res.Code, res.Body.String())
	}
	var untouched int
	if err = a.DB.QueryRow(`SELECT COUNT(*) FROM db_values WHERE property_id=? AND value='4242.5'`, amount.ID).Scan(&untouched); err != nil || untouched != 0 {
		t.Fatalf("failed batch changed values: %d %v", untouched, err)
	}
	payload, _ = json.Marshal(map[string]any{"databaseId": database.ID, "edits": edits})
	res = tableConcurrentCall(a, "POST", "/api/db/bulk-edit", string(payload), a.handleDBBulkEdit)
	if res.Code != 200 {
		t.Fatalf("bulk edit: %d %s", res.Code, res.Body.String())
	}
	var updated []DBRecord
	if err = json.Unmarshal(res.Body.Bytes(), &updated); err != nil || len(updated) != 99 {
		t.Fatalf("bulk edit readback: %v %d", err, len(updated))
	}
	for _, rec := range updated {
		if rec.Values[amount.ID] != 4242.5 || rec.Version != 2 {
			t.Fatalf("bulk edit readback changed: %+v", rec)
		}
	}
	recordsAfterBulk := readAllRecords(t, a, database.ID)
	var computed int
	for _, rec := range recordsAfterBulk {
		if rec.Values[amount.ID] != 4242.5 {
			continue
		}
		computed++
		if rec.Values[formula.ID] != 8485.0 {
			t.Fatalf("formula did not follow bulk value: %+v", rec.Values)
		}
	}
	if computed != 99 {
		t.Fatalf("bulk edit formula rows: %d", computed)
	}
	var stored int
	if err = a.DB.QueryRow(`SELECT COUNT(*) FROM db_values WHERE property_id=? AND value='4242.5'`, amount.ID).Scan(&stored); err != nil || stored != 99 {
		t.Fatalf("bulk edit rows: %d %v", stored, err)
	}

	// A soft-deleted row leaves the table without affecting the other rows.
	res = tableConcurrentCall(a, "DELETE", "/api/db/records?id="+fmt.Sprintf("bulk_rec_%05d", 777), "", a.handleDBRecords)
	if res.Code != 204 {
		t.Fatalf("delete: %d %s", res.Code, res.Body.String())
	}
	if records := readAllRecords(t, a, database.ID); len(records) != bulkRecordCount {
		t.Fatalf("delete count %d", len(records))
	}
	if err = a.DB.Close(); err != nil {
		t.Fatal(err)
	}
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err = a.DB.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity after large restore: %q %v", integrity, err)
	}
	assertSampledBulkRows(t, a, database.ID, notes.ID, amount.ID, formula.ID)
}

func TestConcurrentWritesUpdatesAndImportRetriesStayConsistent(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	database, existing := dbRegressionCreate(t, a)
	notes := computedProperty(t, a, database.ID, "Notes", "text", map[string]any{})

	const writers, perWriter = 8, 20
	created := make(chan string, writers*perWriter)
	problems := make(chan string, 128)
	var creators, readers sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		creators.Add(1)
		go func(writer int) {
			defer creators.Done()
			for i := 0; i < perWriter; i++ {
				body := fmt.Sprintf(`{"databaseId":%q,"title":%q,"values":{%q:%q}}`, database.ID, fmt.Sprintf("Writer %d row %d", writer, i), notes.ID, fmt.Sprintf("payload %d-%d", writer, i))
				res := tableConcurrentCall(a, "POST", "/api/db/records", body, a.handleDBRecords)
				if res.Code != 201 {
					problems <- fmt.Sprintf("create %d-%d: %d %s", writer, i, res.Code, res.Body.String())
					return
				}
				var rec DBRecord
				if err := json.Unmarshal(res.Body.Bytes(), &rec); err != nil || rec.PageID == "" {
					problems <- fmt.Sprintf("create response %d-%d: %v", writer, i, err)
					return
				}
				created <- rec.ID
			}
		}(writer)
	}
	readerDone := make(chan struct{})
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-readerDone:
				return
			default:
				res := tableConcurrentCall(a, "GET", "/api/db/records?databaseId="+database.ID, "", a.handleDBRecords)
				if res.Code != 200 {
					problems <- fmt.Sprintf("concurrent list: %d %s", res.Code, res.Body.String())
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	creators.Wait()
	close(readerDone)
	readers.Wait()
	close(problems)
	for problem := range problems {
		t.Fatal(problem)
	}
	close(created)
	if len(created) != writers*perWriter {
		t.Fatalf("lost concurrent creates: %d", len(created))
	}
	ids := map[string]bool{}
	for id := range created {
		if ids[id] {
			t.Fatalf("duplicate concurrent id: %s", id)
		}
		ids[id] = true
	}
	var count, valueCount int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM db_records WHERE database_id=? AND deleted_at IS NULL`, database.ID).Scan(&count); err != nil || count != writers*perWriter+1 {
		t.Fatalf("concurrent record count: %d %v", count, err)
	}
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM db_values WHERE property_id=?`, notes.ID).Scan(&valueCount); err != nil || valueCount != writers*perWriter {
		t.Fatalf("concurrent value count: %d %v", valueCount, err)
	}

	// Only one of several writers using the same version may win; the others
	// must observe a conflict instead of overwriting the winner.
	title := "Version winner"
	var winners []string
	var updateProblems []string
	var updateMu sync.Mutex
	start := make(chan struct{})
	var updateWG sync.WaitGroup
	for i := 0; i < 4; i++ {
		updateWG.Add(1)
		go func(i int) {
			defer updateWG.Done()
			<-start
			candidate := fmt.Sprintf("Candidate %d", i)
			body, _ := json.Marshal(map[string]any{"id": existing.ID, "version": existing.Version, "title": candidate, "values": map[string]any{notes.ID: candidate}})
			res := tableConcurrentCall(a, "PUT", "/api/db/records", string(body), a.handleDBRecords)
			updateMu.Lock()
			defer updateMu.Unlock()
			switch res.Code {
			case 200:
				winners = append(winners, candidate)
			case 409:
			default:
				updateProblems = append(updateProblems, fmt.Sprintf("%d %s", res.Code, res.Body.String()))
			}
		}(i)
	}
	close(start)
	updateWG.Wait()
	if len(updateProblems) != 0 || len(winners) != 1 {
		t.Fatalf("versioned update: winners=%v problems=%v", winners, updateProblems)
	}
	title = winners[0]
	final, err := a.getDBRecord(existing.ID)
	if err != nil || final.Title != title || final.Version != existing.Version+1 || final.Values[notes.ID] != title {
		t.Fatalf("winning update lost: %+v %v", final, err)
	}

	// Concurrent retries of one import operation must share the receipt.
	plan := requireCSVPlan(t, csvFixture())
	key := "concurrent_csv_operation_123456"
	importStart := make(chan struct{})
	results := make([]csvImportResult, 2)
	importErrs := make([]error, 2)
	var importWG sync.WaitGroup
	for i := 0; i < 2; i++ {
		importWG.Add(1)
		go func(i int) {
			defer importWG.Done()
			<-importStart
			results[i], importErrs[i] = a.importCSV(context.Background(), plan, key)
		}(i)
	}
	close(importStart)
	importWG.Wait()
	for i, err := range importErrs {
		if err != nil {
			t.Fatalf("concurrent import %d: %v", i, err)
		}
	}
	if results[0] != results[1] {
		t.Fatalf("concurrent retries diverged: %+v %+v", results[0], results[1])
	}
	var imports int
	if err = a.DB.QueryRow(`SELECT COUNT(*) FROM databases WHERE title=? AND deleted_at IS NULL`, plan.Request.Title).Scan(&imports); err != nil || imports != 1 {
		t.Fatalf("concurrent import duplicated database: %d %v", imports, err)
	}

	if err = a.DB.Close(); err != nil {
		t.Fatal(err)
	}
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	if final, err = a.getDBRecord(existing.ID); err != nil || final.Title != title || final.Values[notes.ID] != title {
		t.Fatalf("restart lost concurrent winner: %+v %v", final, err)
	}
	if records := readAllRecords(t, a, database.ID); len(records) != writers*perWriter+1 {
		t.Fatalf("restart record count %d", len(records))
	}
}
