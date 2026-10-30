package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func csvFixture() csvImportRequest {
	return csvImportRequest{CSV: "\ufeffName,Notes,Amount,Done,Date,Website,Email\r\n\"First, row\",\"line one\nline two\",12.5,true,2026-10-30,https://example.com,a@example.com\r\n第二行,=SUM(A1:A2),0,false,2026-10-31T10:00:00Z,,\r\nThird,  verbatim  ,,,,,\r\n", Title: "Synthetic CSV", Types: []string{"title", "text", "number", "checkbox", "date", "url", "email"}}
}
func requireCSVPlan(t *testing.T, request csvImportRequest) csvImportPlan {
	t.Helper()
	plan, err := prepareCSVImport(context.Background(), request)
	if err != nil || plan.Preview.ProblemCount != 0 {
		t.Fatal("invalid fixture", err, plan.Preview)
	}
	return plan
}
func checkCSVRecords(t *testing.T, a *AppContext, result csvImportResult) {
	t.Helper()
	db, err := a.getDatabase(result.DatabaseID)
	if err != nil {
		t.Fatal(err)
	}
	if db.PageID != result.PageID || len(db.Properties) != 7 || len(db.Views) != 1 || db.Views[0].Config != "{}" {
		t.Fatal("lost schema", db)
	}
	response := httptest.NewRecorder()
	a.handleDBRecords(response, httptest.NewRequest("GET", "/api/db/records?databaseId="+result.DatabaseID, nil))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var records []DBRecord
	if err = json.Unmarshal(response.Body.Bytes(), &records); err != nil || len(records) != 3 {
		t.Fatal(err, records)
	}
	byTitle := map[string]DBRecord{}
	for _, record := range records {
		byTitle[record.Title] = record
	}
	first := byTitle["First, row"]
	if first.Values[db.Properties[1].ID] != "line one\nline two" || first.Values[db.Properties[2].ID] != 12.5 || first.Values[db.Properties[3].ID] != true || first.Values[db.Properties[4].ID] != "2026-10-30" || first.PageID == "" {
		t.Fatal("lost types/newline/backing page", first)
	}
	second := byTitle["第二行"]
	if second.Values[db.Properties[1].ID] != "=SUM(A1:A2)" || second.Values[db.Properties[2].ID] != float64(0) || second.Values[db.Properties[3].ID] != false {
		t.Fatal("changed literal/formula-looking text or zero/false", second)
	}
	third := byTitle["Third"]
	if third.Values[db.Properties[1].ID] != "  verbatim  " || third.Values[db.Properties[2].ID] != nil || third.Values[db.Properties[3].ID] != nil {
		t.Fatal("coerced empty typed cells", third)
	}
}
func TestCSVImportTypedRetryRestartArchiveAndIndependentViews(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	plan := requireCSVPlan(t, csvFixture())
	key := "csv_roundtrip_operation_123456"
	result, err := a.importCSV(context.Background(), plan, key)
	if err != nil {
		t.Fatal(err)
	}
	checkCSVRecords(t, a, result)
	again, err := a.importCSV(context.Background(), plan, key)
	if err != nil || again != result {
		t.Fatal("retry duplicated import", err)
	}
	copy, err := a.importCSV(context.Background(), plan, "csv_separate_operation_123456")
	if err != nil || copy.DatabaseID == result.DatabaseID {
		t.Fatal("separate copy shared IDs", err)
	}
	if _, err = a.DB.Exec(`UPDATE db_views SET config='{"wrapText":true}' WHERE database_id=?`, copy.DatabaseID); err != nil {
		t.Fatal(err)
	}
	checkCSVRecords(t, a, result)
	a.DB.Close()
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	again, err = a.importCSV(context.Background(), plan, key)
	if err != nil || again != result {
		t.Fatal("restart lost receipt", err)
	}
	checkCSVRecords(t, a, result)
	archive, err := a.createWorkspaceArchive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreWorkspaceArchive(context.Background(), archive, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := initializeDatabase(restored.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	recovered := &AppContext{DB: db, RootPath: restored.Root}
	again, err = recovered.importCSV(context.Background(), plan, key)
	if err != nil || again != result {
		t.Fatal("archive lost receipt", err)
	}
	checkCSVRecords(t, recovered, result)
	changed := csvFixture()
	changed.Title = "Another database"
	different := requireCSVPlan(t, changed)
	if _, err = a.importCSV(context.Background(), different, key); err == nil {
		t.Fatal("receipt accepted changed title")
	}
}
func TestCSVImportStorageFailureAndCancellationRollback(t *testing.T) {
	for _, failure := range []string{"pages", "databases", "db_properties", "db_views", "db_records", "db_values", "app_state", "ignore_value", "corrupt_readback", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			a := setupTestContext(t)
			defer a.DB.Close()
			plan := requireCSVPlan(t, csvFixture())
			tables := []string{"pages", "databases", "db_properties", "db_views", "db_records", "db_values", "app_state"}
			counts := map[string]int{}
			for _, table := range tables {
				var countsValue int
				if err := a.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&countsValue); err != nil {
					t.Fatal(err)
				}
				counts[table] = countsValue
			}
			statement := ""
			switch failure {
			case "cancel":
			case "ignore_value":
				statement = `CREATE TRIGGER csv_fail BEFORE INSERT ON db_values BEGIN SELECT RAISE(IGNORE); END`
			case "corrupt_readback":
				statement = `CREATE TRIGGER csv_fail AFTER INSERT ON db_records BEGIN UPDATE pages SET title='corrupted' WHERE id=NEW.page_id; END`
			default:
				statement = "CREATE TRIGGER csv_fail BEFORE INSERT ON " + failure + " BEGIN SELECT RAISE(ABORT,'synthetic failure'); END"
			}
			if statement != "" {
				if _, err := a.DB.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancel" {
				cancel()
			}
			if _, err := a.importCSV(ctx, plan, "csv_rollback_operation_12345"); err == nil {
				t.Fatal("failed import committed")
			}
			for _, table := range tables {
				var count int
				if err := a.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != counts[table] {
					t.Fatal("partial mutation", table, count, counts[table], err)
				}
			}
		})
	}
}

func TestCSVPreviewRejectsMalformedAmbiguousAndUnsupportedInput(t *testing.T) {
	cases := map[string]string{
		"empty": "", "header only": "Name,Amount\n", "duplicate": "Name,name\nA,1", "blank header": "Name,\nA,1", "header spaces": "Name, Amount\nA,1", "ragged": "Name,Amount\nA,1,2", "bad quotes": "Name,Amount\n\"A,1", "invalid UTF8": "Name,Amount\nA,\xff", "oversize": strings.Repeat("x", maxCSVBytes+1), "too many columns": strings.Repeat("Col,", 32) + "Last\n", "too many rows": "Name\n" + strings.Repeat("row\n", 1001),
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := prepareCSVImport(context.Background(), csvImportRequest{CSV: source, Title: "Test"}); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
	for _, types := range [][]string{{"text", "number"}, {"title", "title"}, {"title", "relation"}, {"title"}, {"title", "select"}} {
		if _, err := prepareCSVImport(context.Background(), csvImportRequest{CSV: "Name,Amount\nRow,1", Title: "Test", Types: types}); err == nil {
			t.Fatal("invalid mapping accepted", types)
		}
	}
	request := csvImportRequest{CSV: "Name,Amount\nRow,00123", Title: "Test"}
	plan := requireCSVPlan(t, request)
	if !reflect.DeepEqual(plan.Preview.Types, []string{"title", "text"}) || plan.Values[0][1] != "00123" {
		t.Fatal("default guessed numeric type")
	}
}
func TestCSVPreviewListsAllTypedErrorsAndCapsResponse(t *testing.T) {
	cases := []struct{ kind, source string }{{"title", " "}, {"title", "line\nbreak"}, {"title", strings.Repeat("a", 501)}, {"text", "bad\x00text"}, {"text", strings.Repeat("a", 32769)}, {"number", "12,000"}, {"number", "9007199254740993"}, {"number", "NaN"}, {"number", "1e999"}, {"number", " 12"}, {"number", "0x1p2"}, {"checkbox", "yes"}, {"date", "10/30/2026"}, {"date", "2026-02-30"}, {"url", "javascript:alert(1)"}, {"email", "not email"}}
	for _, c := range cases {
		t.Run(c.kind+"/"+c.source[:min(len(c.source), 15)], func(t *testing.T) {
			if _, err := csvTypedValue(c.kind, c.source); err == nil {
				t.Fatal("invalid typed value accepted", c)
			}
		})
	}
	plan, err := prepareCSVImport(context.Background(), csvImportRequest{CSV: "Name,Amount\n" + strings.Repeat("Row,invalid\n", 150), Title: "Errors", Types: []string{"title", "number"}})
	if err != nil || plan.Preview.ProblemCount != 150 || len(plan.Preview.Problems) != 100 || plan.Preview.Problems[0].Row != 2 || len(plan.Preview.Sample) != 5 {
		t.Fatal("missing bounded row diagnostics", err, plan.Preview)
	}
	a := setupTestContext(t)
	defer a.DB.Close()
	if _, err = a.importCSV(context.Background(), plan, "csv_invalid_operation_12345"); err == nil {
		t.Fatal("validation blockers were imported")
	}
}
func TestCSVHTTPPreviewDigestMappingAndRetry(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	post := func(request csvImportRequest, preview bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(request)
		url := "/api/import/csv"
		if preview {
			url += "?preview=1"
		}
		w := httptest.NewRecorder()
		a.handleCSVImport(w, httptest.NewRequest("POST", url, strings.NewReader(string(raw))))
		return w
	}
	request := csvFixture()
	before := 0
	a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&before)
	w := post(request, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var preview csvImportPreview
	json.Unmarshal(w.Body.Bytes(), &preview)
	var count int
	a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&count)
	if count != before {
		t.Fatal("preview mutated pages")
	}
	request.Digest = preview.Digest
	request.OperationKey = "csv_http_operation_123456"
	changed := request
	changed.Types = append([]string{}, request.Types...)
	changed.Types[2] = "text"
	if response := post(changed, false); response.Code != 409 {
		t.Fatal("mapping digest ignored", response.Code)
	}
	w = post(request, false)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	again := post(request, false)
	if again.Code != 201 || again.Body.String() != w.Body.String() {
		t.Fatal("HTTP retry duplicated import")
	}
	for _, body := range []string{`{"csv":"a","csv":"b"}`, `{"csv":"a","extra":1}`, `null`, `[]`, `{} {}`} {
		response := httptest.NewRecorder()
		a.handleCSVImport(response, httptest.NewRequest("POST", "/api/import/csv?preview=1", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatal("invalid JSON accepted", body)
		}
	}
}

func TestCSVPreviewJSONExpansionUsesBoundedRouteLimit(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	request := csvImportRequest{Title: "Escaped source", CSV: "Name,Notes\n" + strings.Repeat("Row,"+strings.Repeat("\x01", 16000)+"\n", 50)}
	raw, _ := json.Marshal(request)
	if len(raw) <= 4<<20 || len(request.CSV) > maxCSVBytes {
		t.Fatal("fixture does not exercise JSON expansion")
	}
	response := httptest.NewRecorder()
	handler := localSecurityMiddleware(AppConfig{Port: 8765}, http.HandlerFunc(a.handleCSVImport))
	handler.ServeHTTP(response, httptest.NewRequest("POST", "http://127.0.0.1:8765/api/import/csv?preview=1", bytes.NewReader(raw)))
	if response.Code != 200 {
		t.Fatal("route rejected valid bounded source", response.Code, response.Body.String())
	}
}
