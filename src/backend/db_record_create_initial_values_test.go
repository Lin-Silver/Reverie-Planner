package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestRecordCreateRejectsInitialValuesInsteadOfSilentlyDiscardingThem(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, existing := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	body := fmt.Sprintf(`{"databaseId":%q,"name":"Note","type":"text"}`, database.ID)
	created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", body, app.handleDBProperties)
	if created.Code != http.StatusCreated {
		t.Fatalf("create text property: %d %s", created.Code, created.Body.String())
	}
	var property DBProperty
	if err := json.Unmarshal(created.Body.Bytes(), &property); err != nil {
		t.Fatal(err)
	}
	var recordsBefore, pagesBefore, valuesBefore int
	for _, check := range []struct {
		query string
		into *int
	}{
		{"SELECT COUNT(*) FROM db_records", &recordsBefore},
		{"SELECT COUNT(*) FROM pages", &pagesBefore},
		{"SELECT COUNT(*) FROM db_values", &valuesBefore},
	} {
		if err := app.DB.QueryRow(check.query).Scan(check.into); err != nil {
			t.Fatal(err)
		}
	}
	const recordID = "rec_initial_values_must_not_be_lost"
	request := fmt.Sprintf(`{"id":%q,"databaseId":%q,"title":"Not lost","values":{%q:"Keep me"}}`, recordID, database.ID, property.ID)
	response := dbRegressionCall(t, app, http.MethodPost, "/api/db/records", request, app.handleDBRecords)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("initial values must not be silently dropped: %d %s", response.Code, response.Body.String())
	}
	var recordCount, pageCount, valueCount, attemptedCount int
	for _, check := range []struct {
		query string
		args []any
		into *int
	}{
		{"SELECT COUNT(*) FROM db_records", nil, &recordCount},
		{"SELECT COUNT(*) FROM pages", nil, &pageCount},
		{"SELECT COUNT(*) FROM db_values", nil, &valueCount},
		{"SELECT COUNT(*) FROM db_records WHERE id=?", []any{recordID}, &attemptedCount},
	} {
		if err := app.DB.QueryRow(check.query, check.args...).Scan(check.into); err != nil {
			t.Fatal(err)
		}
	}
	if recordCount != recordsBefore || pageCount != pagesBefore || valueCount != valuesBefore || attemptedCount != 0 {
		t.Fatalf("rejected create changed records/pages/values: %d/%d %d/%d %d/%d attempted=%d",
			recordCount, recordsBefore, pageCount, pagesBefore, valueCount, valuesBefore, attemptedCount)
	}
	var existingVersion int
	if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", existing.ID).Scan(&existingVersion); err != nil {
		t.Fatal(err)
	}
	if existingVersion != existing.Version {
		t.Fatalf("rejected create changed existing record version: %d", existingVersion)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	// A valid empty-values create remains supported; a subsequent versioned
	// update must persist the caller's property value without loss.
	plain := fmt.Sprintf(`{"id":%q,"databaseId":%q,"title":"Not lost","values":{}}`, recordID, database.ID)
	response = dbRegressionCall(t, app, http.MethodPost, "/api/db/records", plain, app.handleDBRecords)
	if response.Code != http.StatusCreated {
		t.Fatalf("empty-values create: %d %s", response.Code, response.Body.String())
	}
	var inserted DBRecord
	if err := json.Unmarshal(response.Body.Bytes(), &inserted); err != nil {
		t.Fatal(err)
	}
	update := fmt.Sprintf(`{"id":%q,"version":%d,"values":{%q:"Keep me"}}`, inserted.ID, inserted.Version, property.ID)
	response = dbRegressionCall(t, app, http.MethodPut, "/api/db/records", update, app.handleDBRecords)
	if response.Code != http.StatusOK {
		t.Fatalf("versioned property update: %d %s", response.Code, response.Body.String())
	}
	var raw string
	if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?", inserted.ID, property.ID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			t.Fatal("record property was not persisted")
		}
		t.Fatal(err)
	}
	if raw != `"Keep me"` {
		t.Fatalf("saved property value changed: %q", raw)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
