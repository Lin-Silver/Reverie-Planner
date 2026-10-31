package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func appearanceCall(t *testing.T, a *AppContext, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	a.handleAppearance(w, httptest.NewRequest(method, "/api/appearance", bytes.NewBufferString(body)))
	return w
}
func TestAppearanceDefaultsCASRestartAndArchive(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	w := appearanceCall(t, a, "GET", "")
	var p workspaceAppearance
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p != defaultAppearance() {
		t.Fatal(w.Code, w.Body.String())
	}
	var count int
	a.DB.QueryRow(`SELECT count(*) FROM app_state WHERE key=?`, appearanceKey).Scan(&count)
	if count != 0 {
		t.Fatal("GET wrote defaults")
	}
	provider := filepath.Join(a.RootPath, "config", "providers.json")
	before, err := os.ReadFile(provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.DB.Exec(`INSERT INTO app_state(key,value) VALUES('favorite-page:synthetic','1')`); err != nil {
		t.Fatal(err)
	}
	w = appearanceCall(t, a, "PUT", `{"style":"classic","wallpaper":"mist","material":"frosted","version":0}`)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Version != 1 || p.Style != "classic" {
		t.Fatal(w.Code, w.Body.String())
	}
	same := appearanceCall(t, a, "PUT", w.Body.String())
	if same.Code != 200 || same.Body.String() != w.Body.String() {
		t.Fatal("no-op changed version")
	}
	stale := appearanceCall(t, a, "PUT", `{"style":"glass","wallpaper":"sakura","material":"soft","version":0}`)
	if stale.Code != 409 {
		t.Fatal("stale write overwrote settings", stale.Body.String())
	}
	a.DB.Close()
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	restored := appearanceCall(t, a, "GET", "")
	if restored.Body.String() != w.Body.String() {
		t.Fatal("restart lost appearance")
	}
	archive, err := a.createWorkspaceArchive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := restoreWorkspaceArchive(context.Background(), archive, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := initializeDatabase(result.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	recovered := appearanceCall(t, &AppContext{DB: db, RootPath: result.Root}, "GET", "")
	if recovered.Body.String() != w.Body.String() {
		t.Fatal("archive lost appearance")
	}
	after, err := os.ReadFile(provider)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("provider changed", err)
	}
	var favorite string
	if err = a.DB.QueryRow(`SELECT value FROM app_state WHERE key='favorite-page:synthetic'`).Scan(&favorite); err != nil || favorite != "1" {
		t.Fatal("unrelated state changed", err)
	}
}
func TestAppearanceRejectsInvalidAndRollsBackReadbackFailure(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	for _, body := range []string{`null`, `[]`, `{}`, `{"style":"glass","wallpaper":"remote-url","material":"balanced","version":0}`, `{"style":"glass","style":"classic","wallpaper":"sakura","material":"soft","version":0}`, `{"style":"glass","wallpaper":"sakura","material":"soft","version":0,"apiKey":"no"}`, `{"style":"glass","wallpaper":"sakura","material":"soft","version":-1}`, `{"style":"glass","wallpaper":"sakura","material":"soft","version":0} {}`} {
		if w := appearanceCall(t, a, "PUT", body); w.Code != 400 {
			t.Fatal("invalid accepted", body, w.Code)
		}
	}
	if _, err := a.DB.Exec(`CREATE TRIGGER appearance_fail AFTER INSERT ON app_state WHEN NEW.key='workspace-appearance:v1' BEGIN UPDATE app_state SET value='corrupt' WHERE key=NEW.key; END`); err != nil {
		t.Fatal(err)
	}
	w := appearanceCall(t, a, "PUT", `{"style":"classic","wallpaper":"mist","material":"soft","version":0}`)
	if w.Code != 500 {
		t.Fatal("readback failure committed", w.Code)
	}
	var count int
	a.DB.QueryRow(`SELECT count(*) FROM app_state WHERE key=?`, appearanceKey).Scan(&count)
	if count != 0 {
		t.Fatal("failed appearance published")
	}
}
