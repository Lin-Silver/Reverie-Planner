package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A workspace archive must keep trash metadata recoverable: entities deleted
// before the backup stay restorable after restoring into a fresh directory and
// restarting. Restores are attempted out of dependency order first so the
// dependency guards are covered on the restored copy, not just the source.
func TestWorkspaceArchiveKeepsTrashRecoverableAcrossRestart(t *testing.T) {
	app := setupTestContext(t)
	defer func() { app.DB.Close() }()
	database, record := dbRegressionCreate(t, app)
	notes := computedProperty(t, app, database.ID, "Notes", "text", map[string]any{})
	computedUpdate(t, app, record, map[string]any{notes.ID: "archived trash value"})
	view := recoveryCreateView(t, app, database.ID, "Archived view")
	uploaded, file := uploadDatabaseTestFile(t, app, database.ID, record.ID, "archived.txt", []byte("keep archived attachment"))
	if uploaded.Code != 201 {
		t.Fatalf("upload: %d %s", uploaded.Code, uploaded.Body.String())
	}
	page := recoveryCreate(t, app, Page{Title: "Archived page", Content: "archived page body", Workspace: true})
	recoveryCall(t, app, "DELETE", "/api/pages?id="+page.ID+"&version=1", nil, app.handlePages, 204)

	recoveryCall(t, app, "DELETE", "/api/db/views?id="+view.ID, nil, app.handleDBViews, 204)
	recoveryCall(t, app, "DELETE", "/api/db/records?id="+record.ID, nil, app.handleDBRecords, 204)
	deletePropertyForGuardTest(t, app, notes.ID, 204)
	recoveryCall(t, app, "DELETE", "/api/databases?id="+database.ID, nil, app.handleDatabases, 204)

	archivePath, err := app.createWorkspaceArchive(context.Background())
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	container := filepath.Join(t.TempDir(), "restored container")
	preview, err := restoreWorkspaceArchive(context.Background(), archivePath, container)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	restoredDB, err := initializeDatabase(preview.Root)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ensureAppConfig(preview.Root)
	if err != nil {
		t.Fatal(err)
	}
	restored := &AppContext{DB: restoredDB, RootPath: preview.Root, Config: cfg}
	defer func() { restored.DB.Close() }()

	// Everything was deleted before the archive, so the restored copy must carry
	// the same tombstones and the same dependency guards as the source.
	restoreDBItem(t, restored, "record", record.ID, 409)
	restoreDBItem(t, restored, "property", notes.ID, 409)
	restoreDBItem(t, restored, "view", view.ID, 409)
	restoreDBItem(t, restored, "database", database.ID, 200)
	restoreDBItem(t, restored, "property", notes.ID, 200)
	restoreDBItem(t, restored, "record", record.ID, 200)
	restoreDBItem(t, restored, "view", view.ID, 200)

	restartRecoveryFixture(t, restored)
	got, err := restored.getDBRecord(record.ID)
	if err != nil || got.Values[notes.ID] != "archived trash value" || got.PageID == "" {
		t.Fatalf("restored record: %+v %v", got, err)
	}
	var storedView DBView
	if err = restored.DB.QueryRow(`SELECT id,database_id,name,type,config,position,version FROM db_views WHERE id=? AND deleted_at IS NULL`, view.ID).Scan(&storedView.ID, &storedView.DatabaseID, &storedView.Name, &storedView.Type, &storedView.Config, &storedView.Position, &storedView.Version); err != nil || storedView.Name != "Archived view" || storedView.Type != "table" {
		t.Fatalf("restored view: %+v %v", storedView, err)
	}
	attachment := recoveryCall(t, restored, "GET", "/api/db/files?id="+file.ID, nil, restored.handleDBFiles, 200)
	if string(attachment.Body.Bytes()) != "keep archived attachment" {
		t.Fatalf("restored attachment bytes: %q", attachment.Body.String())
	}

	// Page trash is part of the archive too; the page returns with its writing.
	list := recoveryCall(t, restored, "GET", "/api/page-trash", nil, restored.handlePageTrash, 200)
	var trash []map[string]any
	if err = json.Unmarshal(list.Body.Bytes(), &trash); err != nil || len(trash) != 1 {
		t.Fatalf("page trash after restore: %v %v", trash, err)
	}
	recoveryCall(t, restored, "POST", "/api/page-trash", map[string]any{"id": page.ID, "version": 1}, restored.handlePageTrash, 200)
	restartedPage, err := fetchPage(restored, page.ID)
	if err != nil || restartedPage.Content != "archived page body" || restartedPage.Title != "Archived page" {
		t.Fatalf("restored page: %+v %v", restartedPage, err)
	}
	restartRecoveryFixture(t, restored)
	restartedPage, err = fetchPage(restored, page.ID)
	if err != nil || restartedPage.Content != "archived page body" {
		t.Fatalf("page after restart: %+v %v", restartedPage, err)
	}
	if got, err = restored.getDBRecord(record.ID); err != nil || got.Values[notes.ID] != "archived trash value" {
		t.Fatalf("record after restart: %+v %v", got, err)
	}

	// The source workspace must keep its own tombstones untouched by the export.
	var sourceActive int
	if err = app.DB.QueryRow(`SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL`, database.ID).Scan(&sourceActive); err != nil || sourceActive != 0 {
		t.Fatalf("archive changed source tombstones: %d %v", sourceActive, err)
	}
	if _, err = os.Stat(filepath.Join(app.RootPath, "data", "attachments", file.ID)); err != nil {
		t.Fatalf("archive removed source attachment: %v", err)
	}
}

func recoveryCreateView(t *testing.T, a *AppContext, databaseID, name string) DBView {
	t.Helper()
	res := recoveryCall(t, a, "POST", "/api/db/views", map[string]string{"databaseId": databaseID, "name": name, "type": "table", "config": "{}"}, a.handleDBViews, 201)
	var view DBView
	if err := json.Unmarshal(res.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view
}
