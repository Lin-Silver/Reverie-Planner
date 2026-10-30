package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func restoreDBItem(t *testing.T, a *AppContext, kind, id string, want int) {
	t.Helper()
	recoveryCall(t, a, "POST", "/api/db/trash", map[string]string{"kind": kind, "id": id}, a.handleDBTrash, want)
}
func restartRecoveryFixture(t *testing.T, a *AppContext) {
	t.Helper()
	if err := a.DB.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
}
func assertTrashedPropertyUnchanged(t *testing.T, a *AppContext, p DBProperty, version int) {
	t.Helper()
	var stored DBProperty
	var deleted sql.NullString
	if err := a.DB.QueryRow("SELECT id,database_id,name,type,config,position,visible,version,deleted_at FROM db_properties WHERE id=?", p.ID).Scan(&stored.ID, &stored.DatabaseID, &stored.Name, &stored.Type, &stored.Config, &stored.Position, &stored.Visible, &stored.Version, &deleted); err != nil {
		t.Fatal(err)
	}
	p.Version = version
	if !deleted.Valid || !reflect.DeepEqual(stored, p) {
		t.Fatalf("failed restore changed saved property: %+v tombstone=%v want=%+v", stored, deleted, p)
	}
}
func TestComputedTrashRestoreRequiresInputsAndSurvivesRestart(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	db, rec := dbRegressionCreate(t, a)
	input := computedProperty(t, a, db.ID, "Amount", "number", map[string]any{})
	formula := computedProperty(t, a, db.ID, "Double", "formula", map[string]any{"operation": "multiply", "args": []any{map[string]any{"propertyId": input.ID}, map[string]any{"number": 2}}})
	computedUpdate(t, a, rec, map[string]any{input.ID: 7})
	providerPath, providerBefore := viewIntegrityProviders(t, a)
	deletePropertyForGuardTest(t, a, formula.ID, 204)
	deletePropertyForGuardTest(t, a, input.ID, 204)
	restartRecoveryFixture(t, a)
	restoreDBItem(t, a, "property", formula.ID, 409)
	assertTrashedPropertyUnchanged(t, a, formula, formula.Version+1)
	restoreDBItem(t, a, "property", input.ID, 200)
	restoreDBItem(t, a, "property", formula.ID, 200)
	// Repeated restore must not increment the version a second time.
	restoreDBItem(t, a, "property", formula.ID, 404)
	restartRecoveryFixture(t, a)
	got, err := a.getDBRecord(rec.ID)
	if err != nil || got.Values[input.ID] != float64(7) || got.Values[formula.ID] != float64(14) {
		t.Fatalf("restored computation: %+v %v", got, err)
	}
	fields, err := a.listProperties(db.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range fields {
		if p.ID == formula.ID && p.Version != formula.Version+2 {
			t.Fatalf("unexpected restore version: %+v", p)
		}
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestRollupRecoveryLifecycleRetainsViewsPagesAndAttachments(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	source, from := dbRegressionCreate(t, a)
	target, to := dbRegressionCreate(t, a)
	price := computedProperty(t, a, target.ID, "Price", "number", map[string]any{})
	relation := computedProperty(t, a, source.ID, "Linked", "relation", map[string]any{"targetDatabaseId": target.ID})
	rollup := computedProperty(t, a, source.ID, "Sum", "rollup", map[string]any{"relationPropertyId": relation.ID, "targetPropertyId": price.ID, "aggregate": "sum"})
	files := computedProperty(t, a, source.ID, "Files", "file", map[string]any{})
	content := []byte("synthetic recovery attachment")
	uploaded, file := uploadDatabaseTestFile(t, a, source.ID, from.ID, "recovery.txt", content)
	if uploaded.Code != 201 {
		t.Fatalf("upload: %d %s", uploaded.Code, uploaded.Body.String())
	}
	computedUpdate(t, a, to, map[string]any{price.ID: 23})
	computedUpdate(t, a, from, map[string]any{relation.ID: []string{to.ID}, files.ID: []string{file.ID}})
	page, err := fetchPage(a, from.PageID)
	if err != nil {
		t.Fatal(err)
	}
	page.Content = "preserve record page writing"
	recoveryCall(t, a, "PUT", "/api/pages", page, a.handlePages, 200)
	viewResponse := recoveryCall(t, a, "POST", "/api/db/views", map[string]string{"databaseId": source.ID, "name": "Retained gallery", "type": "gallery", "config": `{"galleryProperties":["` + files.ID + `"]}`}, a.handleDBViews, 201)
	var view DBView
	if err := json.Unmarshal(viewResponse.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	providerPath, providerBefore := viewIntegrityProviders(t, a)
	deletePropertyForGuardTest(t, a, rollup.ID, 204)
	deletePropertyForGuardTest(t, a, relation.ID, 204)
	deletePropertyForGuardTest(t, a, price.ID, 204)
	recoveryCall(t, a, "DELETE", "/api/db/views?id="+view.ID, nil, a.handleDBViews, 204)
	recoveryCall(t, a, "DELETE", "/api/db/records?id="+from.ID, nil, a.handleDBRecords, 204)
	recoveryCall(t, a, "DELETE", "/api/db/records?id="+to.ID, nil, a.handleDBRecords, 204)
	recoveryCall(t, a, "DELETE", "/api/databases?id="+source.ID, nil, a.handleDatabases, 204)
	recoveryCall(t, a, "DELETE", "/api/databases?id="+target.ID, nil, a.handleDatabases, 204)
	restartRecoveryFixture(t, a)
	restoreDBItem(t, a, "record", from.ID, 409)
	restoreDBItem(t, a, "property", rollup.ID, 409)
	restoreDBItem(t, a, "view", view.ID, 409)
	recoveryCall(t, a, "GET", "/api/db/files?id="+file.ID, nil, a.handleDBFiles, 404)
	restoreDBItem(t, a, "database", source.ID, 200)
	restoreDBItem(t, a, "property", rollup.ID, 409) // relation is deleted
	assertTrashedPropertyUnchanged(t, a, rollup, rollup.Version+1)
	restoreDBItem(t, a, "database", target.ID, 200)
	restoreDBItem(t, a, "record", to.ID, 200)
	restoreDBItem(t, a, "property", relation.ID, 200)
	restoreDBItem(t, a, "property", rollup.ID, 409) // target property is deleted
	assertTrashedPropertyUnchanged(t, a, rollup, rollup.Version+1)
	restoreDBItem(t, a, "property", price.ID, 200)
	restoreDBItem(t, a, "property", rollup.ID, 200)
	restoreDBItem(t, a, "view", view.ID, 200)
	restoreDBItem(t, a, "record", from.ID, 200)
	restartRecoveryFixture(t, a)
	got, err := a.getDBRecord(from.ID)
	if err != nil || got.Values[rollup.ID] != float64(23) || !reflect.DeepEqual(got.Values[relation.ID], []any{to.ID}) || !reflect.DeepEqual(got.Values[files.ID], []any{file.ID}) {
		t.Fatalf("restored relation/rollup/files: %+v %v", got, err)
	}
	writing, err := fetchPage(a, from.PageID)
	if err != nil || writing.Content != page.Content || writing.ID != page.ID {
		t.Fatalf("record page lost: %+v %v", writing, err)
	}
	views, err := a.listViews(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range views {
		if v.ID == view.ID {
			found = true
			if v.Config != view.Config || v.Position != view.Position || v.Version != view.Version+2 {
				t.Fatalf("view changed: %+v", v)
			}
		}
	}
	if !found {
		t.Fatal("restored view missing")
	}
	downloaded := recoveryCall(t, a, "GET", "/api/db/files?id="+file.ID, nil, a.handleDBFiles, 200)
	if !bytes.Equal(downloaded.Body.Bytes(), content) {
		t.Fatal("attachment bytes changed")
	}
	// An attachment remains bound to its original record after recovery.
	otherResponse := recoveryCall(t, a, "POST", "/api/db/records", map[string]string{"databaseId": source.ID, "title": "Unrelated"}, a.handleDBRecords, 201)
	var other DBRecord
	if err := json.Unmarshal(otherResponse.Body.Bytes(), &other); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "PUT", "/api/db/records", map[string]any{"id": other.ID, "title": other.Title, "version": other.Version, "values": map[string]any{files.ID: []string{file.ID}}}, a.handleDBRecords, 400)
	disk, err := os.ReadFile(filepath.Join(a.RootPath, "data", "attachments", file.ID))
	if err != nil || !bytes.Equal(disk, content) {
		t.Fatalf("disk attachment changed: %v", err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestComputedRestoreFailsClosedForCorruptConfigAndSQLFaults(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	db, _ := dbRegressionCreate(t, a)
	input := computedProperty(t, a, db.ID, "Input", "number", map[string]any{})
	formula := computedProperty(t, a, db.ID, "Formula", "formula", map[string]any{"operation": "sum", "args": []any{map[string]any{"propertyId": input.ID}}})
	deletePropertyForGuardTest(t, a, formula.ID, 204)
	for _, raw := range []string{`not-json`, `{"operation":"sum","operation":"multiply","args":[{"number":2}]}`} {
		if _, err := a.DB.Exec("UPDATE db_properties SET config=? WHERE id=?", raw, formula.ID); err != nil {
			t.Fatal(err)
		}
		restoreDBItem(t, a, "property", formula.ID, 409)
	}
	if _, err := a.DB.Exec("UPDATE db_properties SET config=? WHERE id=?", formula.Config, formula.ID); err != nil {
		t.Fatal(err)
	}
	// A dependency query failure must be reported as a storage error, not a
	// dependency conflict that incorrectly suggests restoring user data.
	if _, err := a.DB.Exec("ALTER TABLE db_properties RENAME COLUMN type TO type_unavailable"); err != nil {
		t.Fatal(err)
	}
	err := validateDBComputedDefinition(a.DB, db.ID, "formula", formula.Config)
	if err == nil || errors.Is(err, errInvalidComputedDefinition) {
		t.Fatalf("SQL failure misclassified as a dependency conflict: %v", err)
	}
	recoveryCall(t, a, "POST", "/api/db/properties", map[string]string{"databaseId": db.ID, "name": "Must not create", "type": "formula", "config": formula.Config}, a.handleDBProperties, 500)
	restoreDBItem(t, a, "property", formula.ID, 500)
	if _, err := a.DB.Exec("ALTER TABLE db_properties RENAME COLUMN type_unavailable TO type"); err != nil {
		t.Fatal(err)
	}
	assertTrashedPropertyUnchanged(t, a, formula, formula.Version+1)
	// Fault after validation also rolls back the tombstone and version.
	if _, err := a.DB.Exec(`CREATE TRIGGER reject_computed_restore BEFORE UPDATE OF deleted_at ON db_properties BEGIN SELECT RAISE(ABORT,'synthetic fault'); END`); err != nil {
		t.Fatal(err)
	}
	restoreDBItem(t, a, "property", formula.ID, http.StatusInternalServerError)
	assertTrashedPropertyUnchanged(t, a, formula, formula.Version+1)
	if _, err := a.DB.Exec("DROP TRIGGER reject_computed_restore"); err != nil {
		t.Fatal(err)
	}
	restoreDBItem(t, a, "property", formula.ID, 200)
}

func TestRollupRestoreRejectsRetargetedRelationAndTrashedTargetDatabase(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	source, _ := dbRegressionCreate(t, a)
	target, _ := dbRegressionCreate(t, a)
	other, _ := dbRegressionCreate(t, a)
	price := computedProperty(t, a, target.ID, "Price", "number", map[string]any{})
	relation := computedProperty(t, a, source.ID, "Linked", "relation", map[string]any{"targetDatabaseId": target.ID})
	rollup := computedProperty(t, a, source.ID, "Sum", "rollup", map[string]any{"relationPropertyId": relation.ID, "targetPropertyId": price.ID, "aggregate": "sum"})
	deletePropertyForGuardTest(t, a, rollup.ID, 204)
	relation.Config = `{"targetDatabaseId":"` + other.ID + `"}`
	res := recoveryCall(t, a, "PUT", "/api/db/properties", relation, a.handleDBProperties, 200)
	if err := json.Unmarshal(res.Body.Bytes(), &relation); err != nil {
		t.Fatal(err)
	}
	restoreDBItem(t, a, "property", rollup.ID, 409)
	assertTrashedPropertyUnchanged(t, a, rollup, rollup.Version+1)
	relation.Config = `{"targetDatabaseId":"` + target.ID + `"}`
	recoveryCall(t, a, "PUT", "/api/db/properties", relation, a.handleDBProperties, 200)
	recoveryCall(t, a, "DELETE", "/api/databases?id="+target.ID, nil, a.handleDatabases, 204)
	restartRecoveryFixture(t, a)
	restoreDBItem(t, a, "property", rollup.ID, 409)
	assertTrashedPropertyUnchanged(t, a, rollup, rollup.Version+1)
	restoreDBItem(t, a, "database", target.ID, 200)
	restoreDBItem(t, a, "property", rollup.ID, 200)
}

func TestDatabaseTrashEveryEntityRollsBackAndRestarts(t *testing.T) {
	for _, kind := range []string{"database", "property", "view", "record"} {
		t.Run(kind, func(t *testing.T) {
			a := setupTestContext(t)
			defer func() { a.DB.Close() }()
			db, rec := dbRegressionCreate(t, a)
			property := computedProperty(t, a, db.ID, "Retained input", "text", map[string]any{})
			computedUpdate(t, a, rec, map[string]any{property.ID: "keep synthetic data"})
			viewResponse := recoveryCall(t, a, "POST", "/api/db/views", map[string]string{"databaseId": db.ID, "name": "Retained view", "type": "list", "config": "{}"}, a.handleDBViews, 201)
			var view DBView
			if err := json.Unmarshal(viewResponse.Body.Bytes(), &view); err != nil {
				t.Fatal(err)
			}
			id := map[string]string{"database": db.ID, "property": property.ID, "record": rec.ID, "view": view.ID}[kind]
			table := trashSources[kind].table
			path := map[string]string{"database": "/api/databases", "property": "/api/db/properties", "record": "/api/db/records", "view": "/api/db/views"}[kind]
			handler := map[string]http.HandlerFunc{"database": a.handleDatabases, "property": a.handleDBProperties, "record": a.handleDBRecords, "view": a.handleDBViews}[kind]
			recoveryCall(t, a, "DELETE", path+"?id="+id, nil, handler, 204)
			var beforeVersion, afterVersion int
			var beforeDeleted, afterDeleted string
			if err := a.DB.QueryRow("SELECT version,deleted_at FROM "+table+" WHERE id=?", id).Scan(&beforeVersion, &beforeDeleted); err != nil {
				t.Fatal(err)
			}
			if _, err := a.DB.Exec("CREATE TRIGGER reject_entity_restore BEFORE UPDATE OF deleted_at ON " + table + " BEGIN SELECT RAISE(ABORT,'synthetic write failure'); END"); err != nil {
				t.Fatal(err)
			}
			restoreDBItem(t, a, kind, id, 500)
			if err := a.DB.QueryRow("SELECT version,deleted_at FROM "+table+" WHERE id=?", id).Scan(&afterVersion, &afterDeleted); err != nil {
				t.Fatal(err)
			}
			if beforeVersion != afterVersion || beforeDeleted != afterDeleted {
				t.Fatal("failed restore changed version/tombstone")
			}
			if _, err := a.DB.Exec("DROP TRIGGER reject_entity_restore"); err != nil {
				t.Fatal(err)
			}
			restartRecoveryFixture(t, a)
			restoreDBItem(t, a, kind, id, 200)
			restoreDBItem(t, a, kind, id, 404)
			restartRecoveryFixture(t, a)
			got, err := a.getDBRecord(rec.ID)
			if err != nil || got.Values[property.ID] != "keep synthetic data" {
				t.Fatalf("lifecycle lost values: %+v %v", got, err)
			}
			if err := a.DB.QueryRow("SELECT version FROM "+table+" WHERE id=? AND deleted_at IS NULL", id).Scan(&afterVersion); err != nil || afterVersion != beforeVersion+1 {
				t.Fatalf("restore version: %d %v", afterVersion, err)
			}
		})
	}
}

func TestLinkedPageRecoveryRequiresDatabaseAndViewAndPreservesTrashOnFailure(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	database, _ := dbRegressionCreate(t, a)
	p := recoveryCreate(t, a, Page{Title: "Linked recovery", Content: "Retained linked writing", Workspace: true})
	res := recoveryCall(t, a, "POST", "/api/db/linked-view", map[string]string{"databaseId": database.ID, "pageId": p.ID}, a.handleDBLinkedView, 201)
	var view DBView
	if err := json.Unmarshal(res.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	p, err := fetchPage(a, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "DELETE", fmt.Sprintf("/api/pages?id=%s&version=%d", p.ID, p.Version), nil, a.handlePages, 204)
	recoveryCall(t, a, "DELETE", "/api/db/views?id="+view.ID, nil, a.handleDBViews, 204)
	recoveryCall(t, a, "DELETE", "/api/databases?id="+database.ID, nil, a.handleDatabases, 204)
	restartRecoveryFixture(t, a)
	request := map[string]any{"id": p.ID, "version": p.Version}
	var before string
	if err := a.DB.QueryRow("SELECT snapshot FROM page_trash WHERE id=?", p.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "POST", "/api/page-trash", request, a.handlePageTrash, 409)
	restoreDBItem(t, a, "database", database.ID, 200)
	recoveryCall(t, a, "POST", "/api/page-trash", request, a.handlePageTrash, 409)
	// Storage errors must neither consume the snapshot nor masquerade as a
	// missing dependency. Repair only this disposable fixture to retry.
	if _, err := a.DB.Exec("ALTER TABLE db_views RENAME COLUMN deleted_at TO deleted_at_unavailable"); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "POST", "/api/page-trash", request, a.handlePageTrash, 500)
	if _, err := a.DB.Exec("ALTER TABLE db_views RENAME COLUMN deleted_at_unavailable TO deleted_at"); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := a.DB.QueryRow("SELECT snapshot FROM page_trash WHERE id=?", p.ID).Scan(&after); err != nil || before != after {
		t.Fatalf("failed restore lost snapshot: %v", err)
	}
	restoreDBItem(t, a, "view", view.ID, 200)
	recoveryCall(t, a, "POST", "/api/page-trash", request, a.handlePageTrash, 200)
	restartRecoveryFixture(t, a)
	got, err := fetchPage(a, p.ID)
	if err != nil || got.ID != p.ID || got.LinkedDatabaseID != database.ID || got.LinkedViewID != view.ID || got.Content != p.Content || got.Version != p.Version+1 {
		t.Fatalf("linked recovery changed page: %+v %v", got, err)
	}
	// The restored linked view once again protects against view deletion.
	recoveryCall(t, a, "DELETE", "/api/db/views?id="+view.ID, nil, a.handleDBViews, 409)
}

func TestSelfRelatedRecordRecoveryRequiresOtherTargetsButNotItself(t *testing.T) {
	for _, withOther := range []bool{false, true} {
		t.Run(fmt.Sprint("other target=", withOther), func(t *testing.T) {
			a := setupTestContext(t)
			defer func() { a.DB.Close() }()
			database, record := dbRegressionCreate(t, a)
			relation := computedProperty(t, a, database.ID, "Self reference", "relation", map[string]any{"targetDatabaseId": database.ID})
			ids := []string{record.ID}
			var other DBRecord
			if withOther {
				response := recoveryCall(t, a, "POST", "/api/db/records", map[string]string{"databaseId": database.ID, "title": "Other target"}, a.handleDBRecords, 201)
				if err := json.Unmarshal(response.Body.Bytes(), &other); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, other.ID)
			}
			computedUpdate(t, a, record, map[string]any{relation.ID: ids})
			before, err := a.getDBRecord(record.ID)
			if err != nil {
				t.Fatal(err)
			}
			recoveryCall(t, a, "DELETE", "/api/db/records?id="+record.ID, nil, a.handleDBRecords, 204)
			if withOther {
				recoveryCall(t, a, "DELETE", "/api/db/records?id="+other.ID, nil, a.handleDBRecords, 204)
			}
			restartRecoveryFixture(t, a)
			if withOther {
				restoreDBItem(t, a, "record", record.ID, 409)
				var version int
				var deleted sql.NullString
				if err := a.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?", record.ID).Scan(&version, &deleted); err != nil {
					t.Fatal(err)
				}
				if version != before.Version+1 || !deleted.Valid {
					t.Fatal("failed mixed self/external restore changed record")
				}
				restoreDBItem(t, a, "record", other.ID, 200)
			}
			restoreDBItem(t, a, "record", record.ID, 200)
			restoreDBItem(t, a, "record", record.ID, 404)
			restartRecoveryFixture(t, a)
			got, err := a.getDBRecord(record.ID)
			if err != nil || got.Version != before.Version+2 || !reflect.DeepEqual(got.Values[relation.ID], before.Values[relation.ID]) {
				t.Fatalf("self relation not retained: %+v %v", got, err)
			}
		})
	}
}
