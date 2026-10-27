package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func viewIntegrityProviders(t *testing.T, app *AppContext) (string, []byte) {
	t.Helper()
	path := filepath.Join(app.RootPath, "config", "providers.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, data
}

func assertViewIntegrityProviders(t *testing.T, path string, before []byte) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("AI provider configuration changed: %v", err)
	}
}

func TestDBViewCreateRejectsUnscannablePositionWithoutWriting(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	defaultView := database.Views[0]

	// SQLite promotes max-int64 + 1 to REAL, which cannot be scanned into
	// sql.NullInt64. An ignored scan error previously inserted position zero.
	if _, err := app.DB.Exec("UPDATE db_views SET position=? WHERE id=?", int64(9223372036854775807), defaultView.ID); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"databaseId":%q,"name":"Must not be created","type":"table"}`, database.ID)
	response := dbRegressionCall(t, app, http.MethodPost, "/api/db/views", payload, app.handleDBViews)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("position scan failure must be 500, got %d: %s", response.Code, response.Body.String())
	}
	if count := linkedViewTestCount(t, app, database.ID); count != 1 {
		t.Fatalf("failed position calculation inserted a view: %d", count)
	}
	var position int64
	var version int
	if err := app.DB.QueryRow("SELECT position,version FROM db_views WHERE id=?", defaultView.ID).Scan(&position, &version); err != nil {
		t.Fatal(err)
	}
	if position != 9223372036854775807 || version != defaultView.Version {
		t.Fatalf("existing view changed: position=%d version=%d", position, version)
	}
	saved, err := app.getDBRecord(record.ID)
	if err != nil || saved.Title != record.Title || saved.Version != record.Version {
		t.Fatalf("failed view creation changed record: %+v, %v", saved, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	if _, err := app.DB.Exec("UPDATE db_views SET position=0 WHERE id=?", defaultView.ID); err != nil {
		t.Fatal(err)
	}
	recovered := dbRegressionCall(t, app, http.MethodPost, "/api/db/views", payload, app.handleDBViews)
	if recovered.Code != http.StatusCreated {
		t.Fatalf("valid view creation after fault: %d %s", recovered.Code, recovered.Body.String())
	}
	var created DBView
	if err := json.Unmarshal(recovered.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Position != 1 || linkedViewTestCount(t, app, database.ID) != 2 {
		t.Fatalf("recovered view not persisted in order: %+v", created)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBViewCreateReservesTrashedPositionsForRestore(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, _ := dbRegressionCreate(t, app)
	payload := fmt.Sprintf(`{"databaseId":%q,"name":"Temporary view","type":"table"}`, database.ID)
	first := dbRegressionCall(t, app, http.MethodPost, "/api/db/views", payload, app.handleDBViews)
	if first.Code != http.StatusCreated {
		t.Fatalf("create temporary view: %d %s", first.Code, first.Body.String())
	}
	var deletedView DBView
	if err := json.Unmarshal(first.Body.Bytes(), &deletedView); err != nil {
		t.Fatal(err)
	}
	if deletedView.Position != 1 {
		t.Fatalf("unexpected first view position: %+v", deletedView)
	}
	deleted := dbRegressionCall(t, app, http.MethodDelete, "/api/db/views?id="+deletedView.ID, "", app.handleDBViews)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("soft-delete view: %d %s", deleted.Code, deleted.Body.String())
	}
	next := dbRegressionCall(t, app, http.MethodPost, "/api/db/views", payload, app.handleDBViews)
	if next.Code != http.StatusCreated {
		t.Fatalf("create after soft deletion: %d %s", next.Code, next.Body.String())
	}
	var activeView DBView
	if err := json.Unmarshal(next.Body.Bytes(), &activeView); err != nil {
		t.Fatal(err)
	}
	if activeView.Position != 2 || activeView.ID == deletedView.ID {
		t.Fatalf("trashed position was reused before restore: %+v, deleted=%+v", activeView, deletedView)
	}
	var active, trashed int
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_views WHERE database_id=? AND deleted_at IS NULL", database.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM db_views WHERE database_id=? AND deleted_at IS NOT NULL", database.ID).Scan(&trashed); err != nil {
		t.Fatal(err)
	}
	if active != 2 || trashed != 1 {
		t.Fatalf("view lifecycle changed unexpectedly: active=%d trashed=%d", active, trashed)
	}
	restored := dbRegressionCall(t, app, http.MethodPost, "/api/db/trash", fmt.Sprintf(`{"kind":"view","id":%q}`, deletedView.ID), app.handleDBTrash)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore deleted view: %d %s", restored.Code, restored.Body.String())
	}
	var restoredPosition, restoredVersion int
	if err := app.DB.QueryRow("SELECT position,version FROM db_views WHERE id=? AND deleted_at IS NULL", deletedView.ID).Scan(&restoredPosition, &restoredVersion); err != nil {
		t.Fatal(err)
	}
	if restoredPosition != 1 || restoredVersion != deletedView.Version+2 || activeView.Position != 2 {
		t.Fatalf("view restore did not preserve distinct ordering: restored position=%d version=%d new=%d", restoredPosition, restoredVersion, activeView.Position)
	}
}

func TestDBViewDeleteDistinguishesMissingViewFromSQLiteFailure(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	payload := fmt.Sprintf(`{"databaseId":%q,"name":"Deletable view","type":"table"}`, database.ID)
	added := dbRegressionCall(t, app, http.MethodPost, "/api/db/views", payload, app.handleDBViews)
	if added.Code != http.StatusCreated {
		t.Fatalf("create view: %d %s", added.Code, added.Body.String())
	}
	var view DBView
	if err := json.Unmarshal(added.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	missing := dbRegressionCall(t, app, http.MethodDelete, "/api/db/views?id=missing-view", "", app.handleDBViews)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("absent view should be 404, got %d: %s", missing.Code, missing.Body.String())
	}
	if _, err := app.DB.Exec("ALTER TABLE db_views RENAME TO db_views_unavailable"); err != nil {
		t.Fatal(err)
	}
	failed := dbRegressionCall(t, app, http.MethodDelete, "/api/db/views?id="+view.ID, "", app.handleDBViews)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("SQLite lookup failure should be 500, got %d: %s", failed.Code, failed.Body.String())
	}
	if _, err := app.DB.Exec("ALTER TABLE db_views_unavailable RENAME TO db_views"); err != nil {
		t.Fatal(err)
	}
	var deletedAt *string
	var version int
	if err := app.DB.QueryRow("SELECT deleted_at,version FROM db_views WHERE id=?", view.ID).Scan(&deletedAt, &version); err != nil {
		t.Fatal(err)
	}
	if deletedAt != nil || version != view.Version {
		t.Fatalf("failed view deletion changed tombstone/version: %v %d", deletedAt, version)
	}
	saved, err := app.getDBRecord(record.ID)
	if err != nil || saved.Title != record.Title || saved.Version != record.Version {
		t.Fatalf("failed deletion changed record: %+v %v", saved, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
	recovered := dbRegressionCall(t, app, http.MethodDelete, "/api/db/views?id="+view.ID, "", app.handleDBViews)
	if recovered.Code != http.StatusNoContent {
		t.Fatalf("valid deletion after fault: %d %s", recovered.Code, recovered.Body.String())
	}
	if err := app.DB.QueryRow("SELECT deleted_at,version FROM db_views WHERE id=?", view.ID).Scan(&deletedAt, &version); err != nil {
		t.Fatal(err)
	}
	if deletedAt == nil || version != view.Version+1 {
		t.Fatalf("view deletion not persisted: deletedAt=%v version=%d", deletedAt, version)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}

func TestDBViewUpdateReadbackFailureRollsBackWithoutChangingUserData(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	database, record := dbRegressionCreate(t, app)
	providerPath, providerBefore := viewIntegrityProviders(t, app)
	original := database.Views[0]

	// SQLite allows a TEXT value in this INTEGER-affinity column. An AFTER
	// UPDATE trigger corrupts the version after the UPDATE reports success,
	// making the subsequent response readback fail to scan into an int.
	// Previously that readback was outside a transaction: the client got
	// HTTP 500 even though its view update and the corruption had committed.
	if _, err := app.DB.Exec(`CREATE TRIGGER corrupt_view_readback AFTER UPDATE ON db_views
		WHEN NEW.id = '` + original.ID + `'
		BEGIN UPDATE db_views SET version='invalid-version' WHERE id=NEW.id; END`); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"id":%q,"version":%d,"name":"New view name","type":%q,"config":%q}`,
		original.ID, original.Version, original.Type, original.Config)
	failed := dbRegressionCall(t, app, http.MethodPut, "/api/db/views", payload, app.handleDBViews)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed view readback should return 500, got %d: %s", failed.Code, failed.Body.String())
	}

	var name, kind, config string
	var position, version int
	if err := app.DB.QueryRow(`SELECT name,type,config,position,version FROM db_views WHERE id=?`, original.ID).
		Scan(&name, &kind, &config, &position, &version); err != nil {
		t.Fatalf("failed readback committed a corrupt view: %v", err)
	}
	if name != original.Name || kind != original.Type || config != original.Config ||
		position != original.Position || version != original.Version {
		t.Fatalf("failed readback changed persisted view: name=%q type=%q config=%q position=%d version=%d",
			name, kind, config, position, version)
	}
	saved, err := app.getDBRecord(record.ID)
	if err != nil || saved.Title != record.Title || saved.Version != record.Version {
		t.Fatalf("failed view update changed record: %+v %v", saved, err)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)

	if _, err := app.DB.Exec("DROP TRIGGER corrupt_view_readback"); err != nil {
		t.Fatal(err)
	}
	recovered := dbRegressionCall(t, app, http.MethodPut, "/api/db/views", payload, app.handleDBViews)
	if recovered.Code != http.StatusOK {
		t.Fatalf("view update after removing fault: %d %s", recovered.Code, recovered.Body.String())
	}
	var updated DBView
	if err := json.Unmarshal(recovered.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.Name != "New view name" ||
		updated.Version != original.Version+1 || updated.Position != original.Position {
		t.Fatalf("recovered view response incorrect: %+v", updated)
	}
	if err := app.DB.QueryRow(`SELECT name,version FROM db_views WHERE id=?`, original.ID).Scan(&name, &version); err != nil {
		t.Fatal(err)
	}
	if name != updated.Name || version != updated.Version {
		t.Fatalf("recovered view response differs from SQLite: name=%q version=%d", name, version)
	}
	assertViewIntegrityProviders(t, providerPath, providerBefore)
}
