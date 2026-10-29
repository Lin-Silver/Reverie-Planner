package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func archiveFixture(t *testing.T) (*AppContext, string, dbFileMeta) {
	t.Helper()
	app := setupTestContext(t)
	t.Cleanup(func() { app.DB.Close() })
	db, record := dbRegressionCreate(t, app)
	response, meta := uploadDatabaseTestFile(t, app, db.ID, record.ID, "portable.txt", []byte("Keep these attachment bytes"))
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	if _, err := app.DB.Exec(`INSERT INTO app_state(key,value) VALUES ('favorite-page:' || ?, '1')`, record.PageID); err != nil {
		t.Fatal(err)
	}
	// Trash metadata must remain recoverable in a complete archive too.
	if _, err := app.DB.Exec(`UPDATE db_files SET deleted_at='2026-10-30T00:00:00Z' WHERE id=?`, meta.ID); err != nil {
		t.Fatal(err)
	}
	path, err := app.createWorkspaceArchive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return app, path, meta
}
func readArchiveEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func rewriteArchive(t *testing.T, files map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestWorkspaceArchiveRoundtripPreservesSourceAndAttachments(t *testing.T) {
	app, path, meta := archiveFixture(t)
	originalProviders, err := os.ReadFile(filepath.Join(app.RootPath, "config", "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries := readArchiveEntries(t, path)
	if len(entries) != 4 {
		t.Fatalf("unexpected archive entries: %v", entries)
	}
	for name := range entries {
		if name != "manifest.json" && !validArchiveEntry(name) {
			t.Fatalf("private file exported: %s", name)
		}
	}
	target := filepath.Join(t.TempDir(), "new restored container")
	preview, err := restoreWorkspaceArchive(context.Background(), path, target)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Records != 1 || preview.Attachments != 1 || preview.Root != filepath.Join(target, "workspace") {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	payload, err := os.ReadFile(filepath.Join(preview.Root, "data", "attachments", meta.ID))
	if err != nil || string(payload) != "Keep these attachment bytes" {
		t.Fatalf("attachment restore: %q %v", payload, err)
	}
	restored, err := initializeDatabase(preview.Root)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = restored.QueryRow(`SELECT COUNT(*) FROM app_state WHERE key LIKE 'favorite-page:%' AND value='1'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("favorites missing: %d %v", count, err)
	}
	if _, err = restored.Exec(`UPDATE db_records SET title='Restored copy only'`); err != nil {
		t.Fatal(err)
	}
	restored.Close()
	if err = app.DB.QueryRow(`SELECT COUNT(*) FROM db_records WHERE title='Restored copy only'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("restore changed source: %d %v", count, err)
	}
	after, err := os.ReadFile(filepath.Join(app.RootPath, "config", "providers.json"))
	if err != nil || !bytes.Equal(after, originalProviders) {
		t.Fatal("source credentials changed")
	}
	providers, err := os.ReadFile(filepath.Join(preview.Root, "config", "providers.json"))
	if err != nil || strings.TrimSpace(string(providers)) != "[]" {
		t.Fatal("restored provider config must start empty")
	}
	// Reopen once more to establish restart consistency, including trash file bytes.
	reopened, err := initializeDatabase(preview.Root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	if _, err = restoreWorkspaceArchive(context.Background(), path, target); err == nil {
		t.Fatal("overwrote existing destination")
	}
	if _, err = os.Stat(filepath.Join(preview.Root, "data", "planner.db")); err != nil {
		t.Fatal("existing restore was modified")
	}
}
func TestWorkspaceArchiveRejectsMissingChangedAndSymlinkAttachments(t *testing.T) {
	for _, kind := range []string{"missing", "changed", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			app, _, meta := archiveFixture(t)
			file := filepath.Join(app.RootPath, "data", "attachments", meta.ID)
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if kind == "changed" {
				if err := os.WriteFile(file, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink" {
				other := filepath.Join(app.RootPath, "data", "different.txt")
				os.WriteFile(other, []byte("Keep these attachment bytes"), 0600)
				if err := os.Symlink(other, file); err != nil {
					t.Skip("symlink unavailable on this platform")
				}
			}
			if _, err := app.createWorkspaceArchive(context.Background()); err == nil {
				t.Fatal("export accepted an incomplete or unsafe attachment")
			}
			leftovers, _ := filepath.Glob(filepath.Join(app.RootPath, "data", "backups", "*.pending"))
			if len(leftovers) != 0 {
				t.Fatal("partial export leaked")
			}
		})
	}
}
func TestWorkspaceRestoreRejectsCorruptionAndUnsafeEntries(t *testing.T) {
	_, path, meta := archiveFixture(t)
	cases := map[string]func(map[string][]byte){
		"attachment checksum": func(f map[string][]byte) { f["data/attachments/"+meta.ID] = []byte("bad") },
		"path traversal":      func(f map[string][]byte) { f["../escaped.txt"] = []byte("bad") },
		"windows path":        func(f map[string][]byte) { f[`data\attachments\escape`] = []byte("bad") },
		"credentials":         func(f map[string][]byte) { f["config/providers.json"] = []byte("secret") },
		"missing data":        func(f map[string][]byte) { delete(f, "data/planner.db") },
		"future format": func(f map[string][]byte) {
			var m workspaceManifest
			json.Unmarshal(f["manifest.json"], &m)
			m.Version = 2
			f["manifest.json"], _ = json.Marshal(m)
		},
		"future schema": func(f map[string][]byte) {
			var m workspaceManifest
			json.Unmarshal(f["manifest.json"], &m)
			m.SchemaVersion = latestSupportedSchemaMigration + 1
			f["manifest.json"], _ = json.Marshal(m)
		},
		"invalid database": func(f map[string][]byte) {
			f["data/planner.db"] = []byte("not SQLite")
			sum := sha256.Sum256(f["data/planner.db"])
			var m workspaceManifest
			json.Unmarshal(f["manifest.json"], &m)
			for i := range m.Files {
				if m.Files[i].Path == "data/planner.db" {
					m.Files[i].Size = int64(len(f["data/planner.db"]))
					m.Files[i].SHA256 = hex.EncodeToString(sum[:])
				}
			}
			f["manifest.json"], _ = json.Marshal(m)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			entries := readArchiveEntries(t, path)
			mutate(entries)
			input := rewriteArchive(t, entries)
			target := filepath.Join(t.TempDir(), "refused")
			if _, err := restoreWorkspaceArchive(context.Background(), input, target); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("failed restore left a destination: %v", err)
			}
		})
	}
}
func TestWorkspaceRestoreRejectsDuplicateAndLinkEntries(t *testing.T) {
	_, path, _ := archiveFixture(t)
	for _, kind := range []string{"duplicate", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "bad.zip")
			f, _ := os.Create(input)
			zw := zip.NewWriter(f)
			for name, data := range readArchiveEntries(t, path) {
				w, _ := zw.Create(name)
				w.Write(data)
			}
			header := zip.FileHeader{Name: "data/attachments/link"}
			if kind == "duplicate" {
				header.Name = "manifest.json"
			} else {
				header.SetMode(os.ModeSymlink | 0777)
			}
			w, _ := zw.CreateHeader(&header)
			w.Write([]byte("outside"))
			zw.Close()
			f.Close()
			if _, err := restoreWorkspaceArchive(context.Background(), input, filepath.Join(t.TempDir(), "new")); err == nil {
				t.Fatal("duplicate/link entry accepted")
			}
		})
	}
}
func TestWorkspaceArchiveCancelledRestoreAndExportLeaveSource(t *testing.T) {
	app, path, _ := archiveFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.createWorkspaceArchive(ctx); err == nil {
		t.Fatal("cancelled export succeeded")
	}
	target := filepath.Join(t.TempDir(), "cancelled")
	if _, err := restoreWorkspaceArchive(ctx, path, target); err == nil {
		t.Fatal("cancelled restore succeeded")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("cancelled restore was published")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("original backup deleted")
	}
}
func TestWorkspaceArchiveGateFreezesMutationsWithoutSelfDeadlock(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	entered, release, locked := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler := app.workspaceMutationGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(204) }))
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/pages", nil))
		close(done)
	}()
	<-entered
	go func() { app.WorkspaceMu.Lock(); close(locked); app.WorkspaceMu.Unlock() }()
	select {
	case <-locked:
		t.Fatal("exclusive export passed an active mutation")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-done
	select {
	case <-locked:
	case <-time.After(time.Second):
		t.Fatal("gate never released")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := httptest.NewRecorder()
	app.workspaceMutationGate(http.HandlerFunc(app.handleWorkspaceArchive)).ServeHTTP(result, httptest.NewRequest("POST", "/api/workspace/archive", nil).WithContext(ctx))
	if result.Code != 200 || result.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("archive request failed: %d %s", result.Code, result.Body.String())
	}
}

func TestWorkspaceArchiveCancellationWhileWaitingForMutation(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	app.WorkspaceMu.RLock()
	defer app.WorkspaceMu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := app.createWorkspaceArchive(ctx); err == nil {
		t.Fatal("cancelled export waited indefinitely for a mutation")
	}
}
func TestWorkspaceArchiveRejectsIncompleteApplicationSchema(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	if _, err := app.DB.Exec("ALTER TABLE pages RENAME COLUMN content TO broken_content"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.createWorkspaceArchive(context.Background()); err == nil {
		t.Fatal("export accepted unusable application schema")
	}
}

func TestWorkspaceUploadLimitsAreRouteScoped(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	payload := bytes.Repeat([]byte("x"), 5<<20)
	for _, item := range []struct {
		path string
		want int
	}{{"/api/pages", 413}, {"/api/db/files", 204}, {"/api/workspace/restore-preview", 204}} {
		handler := localSecurityMiddleware(app.Config, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				w.WriteHeader(413)
			} else {
				w.WriteHeader(204)
			}
		}))
		request := httptest.NewRequest("POST", "http://127.0.0.1:8765"+item.path, bytes.NewReader(payload))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != item.want {
			t.Fatalf("%s: %d, expected %d", item.path, response.Code, item.want)
		}
	}
}

func TestWorkspaceLazyReadInitializersParticipateInArchiveGate(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	for _, route := range []string{"/api/workspaces", "/api/members"} {
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		handler := app.workspaceMutationGate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release }))
		go func() { handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", route, nil)); close(done) }()
		<-entered
		unexpected := app.WorkspaceMu.TryLock()
		if unexpected {
			app.WorkspaceMu.Unlock()
		}
		close(release)
		<-done
		if unexpected {
			t.Fatalf("lazy initializer %s bypassed archive mutation gate", route)
		}
	}
}

type archiveFailedReader struct{}

func (archiveFailedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestWorkspaceRestoreUploadErrorsAreNotMisreportedAsSizeLimits(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()
	request := httptest.NewRequest("POST", "/api/workspace/restore-preview", archiveFailedReader{})
	response := httptest.NewRecorder()
	app.handleWorkspaceRestore(response, request)
	if response.Code != 500 || !strings.Contains(response.Body.String(), "could not be staged") {
		t.Fatalf("read failure misreported: %d %s", response.Code, response.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response = httptest.NewRecorder()
	app.handleWorkspaceRestore(response, httptest.NewRequest("POST", "/api/workspace/restore-preview", strings.NewReader("test")).WithContext(ctx))
	if response.Code != 408 {
		t.Fatalf("cancelled upload status: %d", response.Code)
	}
}
