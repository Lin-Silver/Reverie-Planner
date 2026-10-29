package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Plain property configurations must be valid, unambiguous JSON objects,
// even when the current editor does not interpret their optional metadata.
func TestPlainPropertyConfigurationsRejectInvalidJSONWithoutMutation(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	invalidConfigs := []string{
		"not-json",
		"null",
		"[]",
		`{"key":1,"key":2}`,
		`{"nested":{"key":1,"key":2}}`,
		`{"key":1} {"other":2}`,
	}
	kinds := []string{
		"text", "number", "checkbox", "date", "email",
		"url", "person", "file", "created_time", "last_edited_time",
	}
	request := func(method string, property DBProperty) int {
		t.Helper()
		body, err := json.Marshal(property)
		if err != nil {
			t.Fatal(err)
		}
		response := dbRegressionCall(t, app, method, "/api/db/properties", string(body), app.handleDBProperties)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s %s config=%q: expected 400, got %d: %s",
				method, property.Type, property.Config, response.Code, response.Body.String())
		}
		return response.Code
	}

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			name := "Plain " + kind
			valid := DBProperty{
				DatabaseID: database.ID,
				Name: name,
				Type: kind,
				Config: `{"metadata":{"source":"legacy"}}`,
			}
			var initialCount int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", database.ID).Scan(&initialCount); err != nil {
				t.Fatal(err)
			}
			for _, config := range invalidConfigs {
				invalid := valid
				invalid.Config = config
				request(http.MethodPost, invalid)
			}
			var count int
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?", database.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != initialCount {
				t.Fatalf("invalid creates inserted properties: %d -> %d", initialCount, count)
			}

			body, err := json.Marshal(valid)
			if err != nil {
				t.Fatal(err)
			}
			created := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", string(body), app.handleDBProperties)
			if created.Code != http.StatusCreated {
				t.Fatalf("valid %s create: %d %s", kind, created.Code, created.Body.String())
			}
			var saved DBProperty
			if err := json.Unmarshal(created.Body.Bytes(), &saved); err != nil {
				t.Fatal(err)
			}
			for _, config := range invalidConfigs {
				invalid := saved
				invalid.Config = config
				invalid.Name = "Unexpected mutation"
				request(http.MethodPut, invalid)

				var nameAfter, configAfter string
				var versionAfter int
				if err := app.DB.QueryRow("SELECT name,config,version FROM db_properties WHERE id=?", saved.ID).
					Scan(&nameAfter, &configAfter, &versionAfter); err != nil {
					t.Fatal(err)
				}
				if nameAfter != saved.Name || configAfter != saved.Config || versionAfter != saved.Version {
					t.Fatalf("rejected update mutated %s schema: name=%q config=%q version=%d",
						kind, nameAfter, configAfter, versionAfter)
				}
			}
			changed := saved
			changed.Config = `{"metadata":{"source":"updated"}}`
			body, err = json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			updated := dbRegressionCall(t, app, http.MethodPut, "/api/db/properties", string(body), app.handleDBProperties)
			if updated.Code != http.StatusOK {
				t.Fatalf("valid %s update after rejected configs: %d %s", kind, updated.Code, updated.Body.String())
			}
			var recordVersion int
			if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", record.ID).Scan(&recordVersion); err != nil {
				t.Fatal(err)
			}
			if recordVersion != record.Version {
				t.Fatalf("%s schema validation unexpectedly changed record version: %d", kind, recordVersion)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
