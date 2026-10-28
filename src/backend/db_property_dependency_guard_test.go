package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func assertPropertyDeleteGuardState(t *testing.T, app *AppContext, property DBProperty, record DBRecord) {
	t.Helper()
	var version int
	var deleted sql.NullString
	if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_properties WHERE id=?", property.ID).
		Scan(&version, &deleted); err != nil {
		t.Fatal(err)
	}
	if version != property.Version || deleted.Valid {
		t.Fatalf("rejected property deletion changed version/tombstone: %d %v", version, deleted)
	}
	var recordVersion int
	if err := app.DB.QueryRow("SELECT version FROM db_records WHERE id=?", record.ID).
		Scan(&recordVersion); err != nil {
		t.Fatal(err)
	}
	if recordVersion != record.Version {
		t.Fatalf("rejected property deletion changed record version: %d, want %d", recordVersion, record.Version)
	}
}

func deletePropertyForGuardTest(t *testing.T, app *AppContext, id string, want int) {
	t.Helper()
	response := dbRegressionCall(t, app, http.MethodDelete,
		"/api/db/properties?id="+id, "", app.handleDBProperties)
	if response.Code != want {
		t.Fatalf("delete property %s: got %d, want %d: %s",
			id, response.Code, want, response.Body.String())
	}
}

func TestDBPropertyDeleteRestrictsActiveFormulaDependency(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	number := computedProperty(t, app, database.ID, "Amount", "number", map[string]any{})
	formula := computedProperty(t, app, database.ID, "Calculated", "formula", map[string]any{
		"operation": "sum",
		"args": []any{map[string]any{"propertyId": number.ID}},
	})
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	deletePropertyForGuardTest(t, app, number.ID, http.StatusConflict)
	assertPropertyDeleteGuardState(t, app, number, record)
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	deletePropertyForGuardTest(t, app, formula.ID, http.StatusNoContent)
	deletePropertyForGuardTest(t, app, number.ID, http.StatusNoContent)
	var deleted sql.NullString
	if err := app.DB.QueryRow("SELECT deleted_at FROM db_properties WHERE id=?", number.ID).Scan(&deleted); err != nil || !deleted.Valid {
		t.Fatalf("property was not deleted after removing formula: %v %v", deleted, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBPropertyDeleteRestrictsRelationAndCrossDatabaseRollupDependencies(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	source, sourceRecord := dbRegressionCreate(t, app)
	target, targetRecord := dbRegressionCreate(t, app)
	price := computedProperty(t, app, target.ID, "Price", "number", map[string]any{})
	relation := computedProperty(t, app, source.ID, "Linked", "relation",
		map[string]any{"targetDatabaseId": target.ID})
	rollup := computedProperty(t, app, source.ID, "Total", "rollup", map[string]any{
		"relationPropertyId": relation.ID,
		"targetPropertyId": price.ID,
		"aggregate": "sum",
	})
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	deletePropertyForGuardTest(t, app, relation.ID, http.StatusConflict)
	assertPropertyDeleteGuardState(t, app, relation, sourceRecord)
	deletePropertyForGuardTest(t, app, price.ID, http.StatusConflict)
	assertPropertyDeleteGuardState(t, app, price, targetRecord)
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	deletePropertyForGuardTest(t, app, rollup.ID, http.StatusNoContent)
	deletePropertyForGuardTest(t, app, relation.ID, http.StatusNoContent)
	deletePropertyForGuardTest(t, app, price.ID, http.StatusNoContent)
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBPropertyDeleteFailsClosedOnCorruptComputedConfigAndRecovers(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	number := computedProperty(t, app, database.ID, "Value", "number", map[string]any{})
	config, err := json.Marshal(map[string]any{
		"operation": "sum",
		"args": []any{map[string]any{"propertyId": number.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	formula := computedProperty(t, app, database.ID, "Formula", "formula", map[string]any{
		"operation": "sum",
		"args": []any{map[string]any{"propertyId": number.ID}},
	})
	providerPath, providerBefore := viewIntegrityProviders(t, app)

	if _, err := app.DB.Exec("UPDATE db_properties SET config=? WHERE id=?", "invalid-json", formula.ID); err != nil {
		t.Fatal(err)
	}
	deletePropertyForGuardTest(t, app, number.ID, http.StatusInternalServerError)
	assertPropertyDeleteGuardState(t, app, number, record)
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	if _, err := app.DB.Exec("UPDATE db_properties SET config=? WHERE id=?", string(config), formula.ID); err != nil {
		t.Fatal(err)
	}
	deletePropertyForGuardTest(t, app, number.ID, http.StatusConflict)
	assertPropertyDeleteGuardState(t, app, number, record)
	deletePropertyForGuardTest(t, app, formula.ID, http.StatusNoContent)
	deletePropertyForGuardTest(t, app, number.ID, http.StatusNoContent)
	var tombstone sql.NullString
	if err := app.DB.QueryRow("SELECT deleted_at FROM db_properties WHERE id=?", number.ID).Scan(&tombstone); err != nil || !tombstone.Valid {
		t.Fatal(fmt.Errorf("recovered deletion missing tombstone: %v %v", tombstone, err))
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBPropertyDeleteKeepsDependenciesInTrashedDatabase(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	number := computedProperty(t, app, database.ID, "Value", "number", map[string]any{})
	computedProperty(t, app, database.ID, "Dependent", "formula", map[string]any{
		"operation": "sum",
		"args": []any{map[string]any{"propertyId": number.ID}},
	})
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	trashed := dbRegressionCall(t, app, http.MethodDelete,
		"/api/databases?id="+database.ID, "", app.handleDatabases)
	if trashed.Code != http.StatusNoContent {
		t.Fatalf("trash database: %d %s", trashed.Code, trashed.Body.String())
	}

	// A database can be restored later, so its still-active computed
	// properties must continue to protect their dependencies.
	deletePropertyForGuardTest(t, app, number.ID, http.StatusConflict)
	assertPropertyDeleteGuardState(t, app, number, record)
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
