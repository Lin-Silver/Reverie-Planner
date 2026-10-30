package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxCSVBytes = 2 << 20
const maxCSVRequestBytes = 16 << 20 // JSON escaping may expand source bytes sixfold.
const maxCSVRows = 1000
const maxCSVColumns = 32

type csvImportRequest struct {
	CSV          string   `json:"csv"`
	Title        string   `json:"title"`
	Types        []string `json:"types"`
	Digest       string   `json:"digest,omitempty"`
	OperationKey string   `json:"operationKey,omitempty"`
}
type csvImportProblem struct {
	Row     int    `json:"row"` // Logical CSV record, with header at 1; quoted newlines do not add records.
	Column  string `json:"column"`
	Message string `json:"message"`
}
type csvImportPreview struct {
	Digest       string             `json:"digest"`
	Headers      []string           `json:"headers"`
	Types        []string           `json:"types"`
	Rows         int                `json:"rows"`
	Sample       [][]string         `json:"sample"`
	Problems     []csvImportProblem `json:"problems"`
	ProblemCount int                `json:"problemCount"`
}
type csvImportResult struct {
	Digest     string `json:"digest"`
	DatabaseID string `json:"databaseId"`
	PageID     string `json:"pageId"`
	Rows       int    `json:"rows"`
}
type csvImportPlan struct {
	Request csvImportRequest
	Preview csvImportPreview
	Values  [][]any
}

func csvTypedValue(kind, raw string) (any, error) {
	if len(raw) > 32<<10 {
		return nil, errors.New("cell exceeds 32 KiB")
	}
	if strings.ContainsRune(raw, '\x00') {
		return nil, errors.New("NUL is unsupported")
	}
	if kind == "text" {
		return raw, nil
	}
	if kind == "title" {
		if strings.TrimSpace(raw) == "" || len(raw) > 500 || strings.ContainsAny(raw, "\r\n") {
			return nil, errors.New("title must contain 1–500 bytes of non-blank single-line text")
		}
		return raw, nil
	}
	// Empty typed cells are absent, not zero or false. Preserve text verbatim.
	if raw == "" {
		return nil, nil
	}
	if strings.TrimSpace(raw) != raw {
		return nil, errors.New("remove surrounding whitespace or choose Text to preserve it")
	}
	var value any = raw
	switch kind {
	case "number":
		// Accept JSON decimal syntax, not currency/grouping separators or hex floats.
		if !json.Valid([]byte(raw)) || strings.ContainsAny(raw, "[]{}\"tfnu") {
			return nil, errors.New("use a finite decimal number without grouping or currency")
		}
		number, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > 9007199254740991 {
			return nil, errors.New("number is outside the supported finite ±9007199254740991 range; use Text for identifiers")
		}
		value = number
	case "checkbox":
		if raw != "true" && raw != "false" {
			return nil, errors.New("checkbox must be true, false or empty")
		}
		value = raw == "true"
	case "date", "url", "email":
	default:
		return nil, errors.New("unsupported CSV type")
	}
	if err := validatePropertyValue(kind, value); err != nil {
		return nil, err
	}
	if err := validateConfiguredPropertyValue(kind, "{}", value); err != nil {
		return nil, err
	}
	return value, nil
}

// Preview is pure: no staging tables, files, auto-detection or workspace writes.
// Commit rebuilds the same plan and binds the receipt to source + exact mapping.
func prepareCSVImport(ctx context.Context, request csvImportRequest) (csvImportPlan, error) {
	plan := csvImportPlan{Request: request}
	if len(request.CSV) == 0 || len(request.CSV) > maxCSVBytes || !utf8.ValidString(request.CSV) {
		return plan, errors.New("choose a UTF-8 CSV up to 2 MiB")
	}
	if strings.TrimSpace(request.Title) == "" || len(request.Title) > 250 || strings.ContainsRune(request.Title, '\x00') {
		return plan, errors.New("database title must contain 1–250 bytes of non-blank text")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(request.CSV, "\ufeff")))
	headers, err := reader.Read()
	if err != nil {
		return plan, fmt.Errorf("invalid CSV header: %w", err)
	}
	if len(headers) == 0 || len(headers) > maxCSVColumns {
		return plan, errors.New("CSV must have 1–32 columns")
	}
	seen := map[string]bool{}
	for _, header := range headers {
		key := strings.ToLower(strings.TrimSpace(header))
		if key == "" || key != strings.ToLower(header) || len(header) > 120 || strings.ContainsAny(header, "\x00\r\n") || seen[key] {
			return plan, errors.New("headers must be unique non-blank names up to 120 bytes, without surrounding whitespace or line breaks")
		}
		seen[key] = true
	}
	if len(request.Types) == 0 {
		request.Types = make([]string, len(headers))
		for i := range request.Types {
			request.Types[i] = "text"
		}
		request.Types[0] = "title"
	}
	if len(request.Types) != len(headers) {
		return plan, errors.New("choose one type per CSV column")
	}
	titles := 0
	for _, kind := range request.Types {
		switch kind {
		case "title":
			titles++
		case "text", "number", "checkbox", "date", "url", "email":
		default:
			return plan, fmt.Errorf("unsupported CSV type: %s", kind)
		}
	}
	if titles != 1 {
		return plan, errors.New("choose exactly one Title column")
	}
	plan.Request = request
	// Exclude operation/digest fields so retries and preflights share the same identity.
	identity, _ := json.Marshal(struct {
		CSV   string
		Title string
		Types []string
	}{request.CSV, request.Title, request.Types})
	hash := sha256.Sum256(identity)
	plan.Preview = csvImportPreview{Digest: hex.EncodeToString(hash[:]), Headers: headers, Types: request.Types, Sample: [][]string{}, Problems: []csvImportProblem{}}
	for {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return plan, fmt.Errorf("invalid CSV near record %d: %w", plan.Preview.Rows+2, err)
		}
		if plan.Preview.Rows >= maxCSVRows {
			return plan, errors.New("CSV exceeds 1000 data records; split the file")
		}
		plan.Preview.Rows++
		if len(plan.Preview.Sample) < 5 {
			plan.Preview.Sample = append(plan.Preview.Sample, record)
		}
		values := make([]any, len(record))
		for i, raw := range record {
			value, err := csvTypedValue(request.Types[i], raw)
			if err != nil {
				plan.Preview.ProblemCount++
				if len(plan.Preview.Problems) < 100 {
					plan.Preview.Problems = append(plan.Preview.Problems, csvImportProblem{plan.Preview.Rows + 1, headers[i], err.Error()})
				}
			} else {
				values[i] = value
			}
		}
		plan.Values = append(plan.Values, values)
	}
	if plan.Preview.Rows == 0 {
		return plan, errors.New("CSV needs at least one data record")
	}
	return plan, nil
}

func (a *AppContext) importCSV(ctx context.Context, plan csvImportPlan, key string) (csvImportResult, error) {
	result := csvImportResult{Digest: plan.Preview.Digest, Rows: plan.Preview.Rows}
	if plan.Preview.ProblemCount > 0 {
		return result, errors.New("fix all CSV validation problems before importing")
	}
	if !importKeyPattern.MatchString(key) {
		return result, errors.New("invalid import operation key")
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	receipt := "csv_import:" + key
	var saved string
	err = tx.QueryRowContext(ctx, "SELECT value FROM app_state WHERE key=?", receipt).Scan(&saved)
	if err == nil {
		if err = json.Unmarshal([]byte(saved), &result); err != nil {
			return result, err
		}
		if result.Digest != plan.Preview.Digest {
			return result, errors.New("operation key belongs to different CSV data or mapping")
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	result.DatabaseID = newID("db")
	result.PageID = newID("page")
	now := timeNowUTC()
	insertPage := func(id, title string) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO pages(id,title,content,workspace,created_by,version,created_at,updated_at) VALUES(?,?,'',1,'Import',1,?,?)`, id, title, now, now)
		return e
	}
	if err = insertPage(result.PageID, plan.Request.Title); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO databases(id,page_id,title,version,created_at,updated_at) VALUES(?,?,?,1,?,?)`, result.DatabaseID, result.PageID, plan.Request.Title, now, now); err != nil {
		return result, err
	}
	properties := make([]string, len(plan.Preview.Headers))
	titleIndex := 0
	for i, name := range plan.Preview.Headers {
		properties[i] = newID("prop")
		if plan.Request.Types[i] == "title" {
			titleIndex = i
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO db_properties(id,database_id,name,type,config,position,visible,version) VALUES(?,?,?,?,'{}',?,1,1)`, properties[i], result.DatabaseID, name, plan.Request.Types[i], i); err != nil {
			return result, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO db_views(id,database_id,name,type,config,position,version) VALUES(?,?,'Table','table','{}',0,1)`, newID("view"), result.DatabaseID); err != nil {
		return result, err
	}
	for _, row := range plan.Values {
		recordID, pageID := newID("rec"), newID("page")
		title := row[titleIndex].(string)
		if err = insertPage(pageID, title); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO db_records(id,database_id,title,page_id,version,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`, recordID, result.DatabaseID, title, pageID, now, now); err != nil {
			return result, err
		}
		for i, value := range row {
			if i == titleIndex || value == nil {
				continue
			}
			encoded, e := json.Marshal(value)
			if e != nil {
				return result, e
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO db_values(record_id,property_id,value,version,updated_at) VALUES(?,?,?,1,?)`, recordID, properties[i], string(encoded), now); err != nil {
				return result, err
			}
		}
		created, e := readDBRecordInTx(tx, recordID, true)
		if e != nil {
			return result, e
		}
		if created.Title != title || created.PageID != pageID {
			return result, errors.New("import record readback changed")
		}
		for i, value := range row {
			if i == titleIndex {
				continue
			}
			actual := created.Values[properties[i]]
			before, _ := json.Marshal(value)
			after, _ := json.Marshal(actual)
			if string(before) != string(after) {
				return result, errors.New("import value readback changed")
			}
		}
	}
	// Read complete schema before publishing; a broken readback also rolls back.
	createdDatabase, err := readDatabaseInTx(tx, result.DatabaseID)
	if err != nil {
		return result, err
	}
	if createdDatabase.PageID != result.PageID || createdDatabase.Title != plan.Request.Title || len(createdDatabase.Properties) != len(properties) || len(createdDatabase.Views) != 1 || createdDatabase.Views[0].Type != "table" || createdDatabase.Views[0].Config != "{}" {
		return result, errors.New("import schema readback changed")
	}
	for i, property := range createdDatabase.Properties {
		if property.ID != properties[i] || property.Name != plan.Preview.Headers[i] || property.Type != plan.Request.Types[i] {
			return result, errors.New("import property readback changed")
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO app_state(key,value) VALUES(?,?)", receipt, string(encoded)); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (a *AppContext) handleCSVImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSONError(w, 405, "method not allowed")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxCSVRequestBytes+1))
	if err != nil || len(raw) > maxCSVRequestBytes {
		writeJSONError(w, 400, "unable to read CSV request within 16 MiB limit")
		return
	}
	if !utf8.Valid(raw) || rejectDuplicateJSONKeys(raw) != nil {
		writeJSONError(w, 400, "invalid request JSON")
		return
	}
	var request csvImportRequest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil || decoder.Decode(new(any)) != io.EOF {
		writeJSONError(w, 400, "invalid CSV request JSON")
		return
	}
	plan, err := prepareCSVImport(r.Context(), request)
	if err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("preview") == "1" {
		writeJSON(w, 200, plan.Preview)
		return
	}
	if request.Digest != plan.Preview.Digest {
		writeJSONError(w, 409, "CSV or mapping changed; preview again")
		return
	}
	result, err := a.importCSV(r.Context(), plan, request.OperationKey)
	if err != nil {
		writeJSONError(w, 400, "Import was not applied: "+err.Error())
		return
	}
	writeJSON(w, 201, result)
}
