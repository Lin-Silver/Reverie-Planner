package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assetImportFixture(t *testing.T) (markdownPreview, []markdownBlocks) {
	t.Helper()
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, map[string]string{"Page.md": "# Page", "Page/image.png": string(image), "Page/notes.txt": "preserved offline bytes", "unused.pdf": "%PDF-synthetic"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Assets) != 3 || len(preview.Blockers) != 0 {
		t.Fatalf("wrong asset preview: %+v", preview)
	}
	blocks := []markdownBlocks{{Path: "Page.md", Content: json.RawMessage(`[{"type":"image","props":{"url":"Page/image.png","caption":"Pixel"}},{"type":"paragraph","content":[{"type":"link","href":"Page/notes.txt","content":[{"type":"text","text":"Notes"}]}]}]`)}}
	return preview, blocks
}
func readTestAsset(t *testing.T, a *AppContext, id string, want int) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	a.handlePageAssets(response, httptest.NewRequest("GET", "/api/page-assets?id="+id, nil))
	if response.Code != want {
		t.Fatalf("asset status %d instead of %d: %s", response.Code, want, response.Body.String())
	}
	return response
}
func assetIDForName(t *testing.T, a *AppContext, name string) string {
	t.Helper()
	var id string
	if err := a.DB.QueryRow("SELECT id FROM page_assets WHERE original_name=?", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestMarkdownAssetsAtomicRetryRestartTrashAndArchive(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	preview, blocks := assetImportFixture(t)
	encoded, _ := json.Marshal(preview)
	if bytes.Contains(encoded, []byte("preserved offline bytes")) {
		t.Fatal("preview leaked asset payload")
	}
	result, err := a.importMarkdown(context.Background(), preview, blocks, "asset_operation_123456")
	if err != nil {
		t.Fatal(err)
	}
	var rootContent string
	if err = a.DB.QueryRow("SELECT content FROM pages WHERE id=?", result.RootID).Scan(&rootContent); err != nil || !strings.Contains(rootContent, "unused.pdf") || !strings.Contains(rootContent, "Imported attachments") {
		t.Fatal("attachment index lost", err)
	}
	imageID := assetIDForName(t, a, "image.png")
	textID := assetIDForName(t, a, "notes.txt")
	response := readTestAsset(t, a, textID, 200)
	if response.Body.String() != "preserved offline bytes" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment") || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("unsafe download headers or changed bytes")
	}
	response = readTestAsset(t, a, imageID, 200)
	if response.Header().Get("Content-Type") != "image/png" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(response.Header())
	}
	var content string
	if err = a.DB.QueryRow("SELECT content FROM pages WHERE id=?", result.Pages["Page.md"]).Scan(&content); err != nil || !strings.Contains(content, "/api/page-assets?id="+imageID) || !strings.Contains(content, textID) {
		t.Fatal("links not rewritten", content, err)
	}
	a.DB.Close()
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.importMarkdown(context.Background(), preview, blocks, "asset_operation_123456")
	if err != nil || again.RootID != result.RootID {
		t.Fatal("receipt changed", err)
	}
	var count int
	if err = a.DB.QueryRow("SELECT count(*) FROM page_assets").Scan(&count); err != nil || count != 3 {
		t.Fatal("retry duplicated files", count, err)
	}
	// Physical page deletion is captured by the existing trash triggers. Asset
	// retention must not block it, and the download is hidden until root recovery.
	if _, err = a.DB.Exec("DELETE FROM pages WHERE id=?", result.Pages["Page.md"]); err != nil {
		t.Fatal(err)
	}
	if _, err = a.DB.Exec("DELETE FROM pages WHERE id=?", result.RootID); err != nil {
		t.Fatal(err)
	}
	readTestAsset(t, a, textID, 404)
	archive, err := a.createWorkspaceArchive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreWorkspaceArchive(context.Background(), archive, filepath.Join(t.TempDir(), "restored container"))
	if err != nil {
		t.Fatal(err)
	}
	if restored.Attachments != 3 {
		t.Fatal("archive did not count SQLite assets", restored)
	}
	db, err := initializeDatabase(restored.Root)
	if err != nil {
		t.Fatal(err)
	}
	copy := &AppContext{DB: db, RootPath: restored.Root}
	defer copy.DB.Close()
	readTestAsset(t, copy, textID, 404)
	request := httptest.NewRequest("POST", "/api/page-trash", strings.NewReader(`{"id":"`+result.RootID+`","version":1}`))
	response = httptest.NewRecorder()
	copy.handlePageTrash(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if readTestAsset(t, copy, textID, 200).Body.String() != "preserved offline bytes" {
		t.Fatal("restored bytes changed")
	}
	if _, err = verifyPageAssetsData(context.Background(), copy.DB); err != nil {
		t.Fatal(err)
	}
	// Source root remains in trash; restoring its copy does not touch source data.
	readTestAsset(t, a, textID, 404)
}
func TestMarkdownAssetsStorageFailureAndCancellationRollBack(t *testing.T) {
	for _, failure := range []string{"asset", "receipt", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			a := setupTestContext(t)
			defer a.DB.Close()
			preview, blocks := assetImportFixture(t)
			table, condition := "page_assets", "NEW.original_name='notes.txt'"
			if failure == "receipt" {
				table = "app_state"
				condition = "NEW.key LIKE 'markdown_import:%'"
			}
			if failure != "cancel" {
				if _, err := a.DB.Exec("CREATE TRIGGER fail_asset BEFORE INSERT ON " + table + " WHEN " + condition + " BEGIN SELECT RAISE(ABORT,'synthetic failure'); END"); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancel" {
				cancel()
			}
			if _, err := a.importMarkdown(ctx, preview, blocks, "asset_rollback_1234567"); err == nil {
				t.Fatal("failure accepted")
			}
			for _, table := range []string{"pages", "page_assets", "app_state"} {
				var count int
				if err := a.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatal("partial import", table, count, err)
				}
			}
		})
	}
}
func TestMarkdownAssetsRejectUnsafeFormatsURLsAndSize(t *testing.T) {
	preview, blocks := assetImportFixture(t)
	for _, link := range []string{"https://example.com/image.png", "//example.com/image.png", "../image.png", "Page/missing.png", "Page/notes.txt", "Page/image.png#heading", "Page/image.png?x=1", "/api/page-assets?id=existing", "data:image/png;base64,AAAA"} {
		raw, _ := json.Marshal([]any{map[string]any{"type": "image", "props": map[string]any{"url": link}}})
		assets := map[string]string{}
		for _, a := range preview.Assets {
			assets[a.Path] = "/api/page-assets?id=preview"
		}
		if _, err := convertMarkdownPages(preview, []markdownBlocks{{Path: blocks[0].Path, Content: raw}}, map[string]string{"Page.md": "page"}, assets); err == nil {
			t.Fatal("unsafe media accepted", link)
		}
	}
	for _, name := range []string{"active.svg", "active.html", "run.js", "Database.csv", "nested.zip", "fake.png"} {
		p, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, map[string]string{"A.md": "text", name: "active"}))
		if err != nil || len(p.Blockers) != 1 {
			t.Fatal("unsupported asset not reported", name, err)
		}
	}
	if _, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, map[string]string{"A.md": "text", "big.txt": strings.Repeat("x", maxMarkdownAssetBytes+1)})); err == nil {
		t.Fatal("oversized asset accepted")
	}
}
func TestPageAssetsIntegrityAndSchemaGuard(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	preview, blocks := assetImportFixture(t)
	if _, err := a.importMarkdown(context.Background(), preview, blocks, "asset_integrity_1234567"); err != nil {
		t.Fatal(err)
	}
	id := assetIDForName(t, a, "notes.txt")
	if _, err := a.DB.Exec("UPDATE page_assets SET sha256='broken' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	readTestAsset(t, a, id, 500)
	if _, err := a.createWorkspaceArchive(context.Background()); err == nil {
		t.Fatal("corrupt attachment exported")
	}
	if _, err := a.DB.Exec("DROP TABLE page_assets"); err != nil {
		t.Fatal(err)
	}
	a.DB.Close()
	if db, err := initializeDatabase(a.RootPath); err == nil {
		db.Close()
		t.Fatal("applied migration silently repaired")
	}
}

func TestPageAssetsMigration12PreservesV11WritingAndHistory(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	for _, query := range []string{`INSERT INTO pages(id,title,content,workspace) VALUES('older-page','Keep title','Original writing',1)`, `UPDATE pages SET content='Newer writing',version=2 WHERE id='older-page'`, `DROP TABLE page_assets`, `DELETE FROM schema_migrations WHERE version=12`} {
		if _, err := a.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	a.DB.Close()
	var err error
	for i := 0; i < 2; i++ {
		a.DB, err = initializeDatabase(a.RootPath)
		if err != nil {
			t.Fatal(err)
		}
		var writing, snapshot string
		if err = a.DB.QueryRow("SELECT content FROM pages WHERE id='older-page'").Scan(&writing); err != nil || writing != "Newer writing" {
			t.Fatal("writing changed", err)
		}
		if err = a.DB.QueryRow("SELECT snapshot FROM page_revisions WHERE page_id='older-page'").Scan(&snapshot); err != nil || !strings.Contains(snapshot, "Original writing") {
			t.Fatal("history lost", err)
		}
		if i == 0 {
			a.DB.Close()
		}
	}
}
func TestPageAssetsRestoreRejectsCorruptBytesAndMissingOwner(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	preview, blocks := assetImportFixture(t)
	if _, err := a.importMarkdown(context.Background(), preview, blocks, "asset_archive_12345678"); err != nil {
		t.Fatal(err)
	}
	archive, err := a.createWorkspaceArchive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`UPDATE page_assets SET data=zeroblob(size_bytes) WHERE original_name='notes.txt'`, `UPDATE page_assets SET root_id='missing-owner'`} {
		t.Run(query, func(t *testing.T) {
			entries := readArchiveEntries(t, archive)
			dbPath := filepath.Join(t.TempDir(), "damaged.db")
			if err := os.WriteFile(dbPath, entries["data/planner.db"], 0600); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(query); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			entries["data/planner.db"], err = os.ReadFile(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(entries["data/planner.db"])
			var manifest workspaceManifest
			if err = json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
				t.Fatal(err)
			}
			for i := range manifest.Files {
				if manifest.Files[i].Path == "data/planner.db" {
					manifest.Files[i].Size = int64(len(entries["data/planner.db"]))
					manifest.Files[i].SHA256 = hex.EncodeToString(sum[:])
				}
			}
			entries["manifest.json"], err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "refused")
			if _, err = restoreWorkspaceArchive(context.Background(), rewriteArchive(t, entries), target); err == nil {
				t.Fatal("corrupt SQLite asset restored")
			}
			if _, err = os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("failed restore published directory", err)
			}
		})
	}
}
