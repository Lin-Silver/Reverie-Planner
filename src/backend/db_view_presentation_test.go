package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestDBViewPresentationValidationPreservesData(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	original := database.Views[0]
	configs := []string{
		`{"filter":{}}`, `{"filter":null}`, `{"sort":[]}`, `{"groupBy":true}`,
		`{"dateProperty":2}`, `{"columnOrder":null}`, `{"columnOrder":["a","a"]}`,
		`{"hiddenColumns":[{}]}`, `{"columnWidths":[]}`, `{"columnWidths":{"a":"wide"}}`,
		`{"columnWidths":{"a":99}}`, `{"columnWidths":{"a":701}}`,
		`{"filterTree":null}`, `{"filterTree":{"kind":"group","join":"and","children":null}}`,
		`{"filterTree":{"kind":"group","join":"and","children":[null]}}`,
		`{"filterTree":{"kind":"group","join":"invalid","children":[]}}`,
		`{"filterTree":{"kind":"group","join":"or","children":[{"kind":"rule","propertyId":"a","operator":"gt","value":{}}]}}`,
	}
	for _, config := range configs {
		t.Run(config, func(t *testing.T) {
			candidate := original
			candidate.Name = "Must not persist"
			candidate.Config = config
			payload, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodPost, http.MethodPut} {
				response := dbRegressionCall(t, app, method, "/api/db/views", string(payload), app.handleDBViews)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("%s got %d: %s", method, response.Code, response.Body.String())
				}
			}
			var name, configAfter string
			var version, count int
			if err := app.DB.QueryRow("SELECT name,config,version FROM db_views WHERE id=?", original.ID).Scan(&name, &configAfter, &version); err != nil {
				t.Fatal(err)
			}
			if name != original.Name || configAfter != original.Config || version != original.Version {
				t.Fatal("rejected config changed existing view")
			}
			if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_views WHERE database_id=?", database.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("view count %d: %v", count, err)
			}
			saved, err := app.getDBRecord(record.ID)
			if err != nil || saved.Title != record.Title || saved.Version != record.Version {
				t.Fatalf("record changed: %+v %v", saved, err)
			}
			assertViewIntegrityProviders(t, providerPath, providerBefore)
		})
	}
	valid := original
	valid.Config = `{"filter":"test","columnOrder":["a"],"hiddenColumns":[],"columnWidths":{"a":160},"filterTree":{"kind":"group","join":"and","children":[]},"extension":{"future":true}}`
	payload, _ := json.Marshal(valid)
	response := dbRegressionCall(t, app, http.MethodPut, "/api/db/views", string(payload), app.handleDBViews)
	if response.Code != http.StatusOK {
		t.Fatalf("valid config rejected: %d %s", response.Code, response.Body.String())
	}
}
