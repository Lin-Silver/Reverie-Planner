package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func documentFixture(t *testing.T, a *AppContext) (markdownImportResult, documentPackage) {
	t.Helper()
	preview, blocks := assetImportFixture(t)
	imported, err := a.importMarkdown(context.Background(), preview, blocks, "document_fixture_123456")
	if err != nil {
		t.Fatal(err)
	}
	// Include structured child links, rich styles and a number that must not be
	// rounded through float64 when links are rewritten.
	page := imported.Pages["Page.md"]
	var content string
	if err = a.DB.QueryRow("SELECT content FROM pages WHERE id=?", page).Scan(&content); err != nil {
		t.Fatal(err)
	}
	child := `[{"type":"paragraph","content":[{"type":"text","text":"Child writing","styles":{"bold":true}}],"props":{"testNumber":9007199254740993}}]`
	if _, err = a.DB.Exec(`INSERT INTO pages(id,parent_id,title,icon,content,workspace) VALUES('doc-child',?,'Child','📓',?,1)`, page, child); err != nil {
		t.Fatal(err)
	}
	content = strings.TrimSuffix(content, "]") + `,{"type":"paragraph","content":[{"type":"link","href":"/?page=doc-child","content":[{"type":"text","text":"Open child","styles":{}}]}]}]`
	if _, err = a.DB.Exec("UPDATE pages SET content=? WHERE id=?", content, page); err != nil {
		t.Fatal(err)
	}
	p, err := a.exportDocumentPackage(context.Background(), imported.RootID)
	if err != nil {
		t.Fatal(err)
	}
	return imported, p
}
func TestDocumentPackageRoundtripRetryRestartAndArchive(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	source, p := documentFixture(t, a)
	if len(p.Pages) != 3 || len(p.Assets) != 3 {
		t.Fatal("missing documents or unreferenced assets", p)
	}
	raw, _ := json.Marshal(p)
	parsed, err := parseDocumentPackage(raw)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.importDocumentPackage(context.Background(), parsed, "digest", "document_roundtrip_12345")
	if err != nil {
		t.Fatal(err)
	}
	var parent, content, icon string
	if err = a.DB.QueryRow("SELECT parent_id,content,icon FROM pages WHERE id=?", result.Pages["doc-child"]).Scan(&parent, &content, &icon); err != nil || parent != result.Pages[source.Pages["Page.md"]] || !strings.Contains(content, "9007199254740993") || icon != "📓" {
		t.Fatal("lost hierarchy, precision or icon", parent, content, icon, err)
	}
	exported, err := a.exportDocumentPackage(context.Background(), result.Pages[p.RootID])
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Pages) != 3 || len(exported.Assets) != 3 {
		t.Fatal("roundtrip lost data")
	}
	// Inverse identity rewrite proves all editor JSON and typed numbers survive;
	// generated page/asset IDs and wrapper metadata are intentionally independent.
	inversePages, inverseAssets := map[string]string{}, map[string]string{}
	for old, id := range result.Pages {
		inversePages[id] = old
	}
	for _, asset := range exported.Assets {
		for _, old := range p.Assets {
			if old.SHA256 == asset.SHA256 {
				inverseAssets[asset.ID] = old.ID
			}
		}
	}
	for _, copy := range exported.Pages {
		var original documentPackagePage
		for _, page := range p.Pages {
			if result.Pages[page.ID] == copy.ID {
				original = page
			}
		}
		normalized, e := rewriteDocumentContent(copy.Content, inversePages, inverseAssets)
		if e != nil {
			t.Fatal(e)
		}
		before, e := rewriteDocumentContent(original.Content, mapIDs(p.Pages), assetIDs(p.Assets))
		if e != nil || before != normalized || copy.Title != original.Title {
			t.Fatal("lossy roundtrip", original.Title, before, normalized, e)
		}
	}
	a.DB.Close()
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.importDocumentPackage(context.Background(), parsed, "digest", "document_roundtrip_12345")
	if err != nil || again.RootID != result.RootID {
		t.Fatal("restart retry duplicated copy", err)
	}
	archive, err := a.createWorkspaceArchive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreWorkspaceArchive(context.Background(), archive, filepath.Join(t.TempDir(), "recovered"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := initializeDatabase(restored.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	copy := &AppContext{DB: db, RootPath: restored.Root}
	again, err = copy.importDocumentPackage(context.Background(), parsed, "digest", "document_roundtrip_12345")
	if err != nil || again.RootID != result.RootID {
		t.Fatal("archive lost receipt", err)
	}
	if _, err = copy.exportDocumentPackage(context.Background(), result.Pages[p.RootID]); err != nil {
		t.Fatal("restored documents not exportable", err)
	}
	if _, err = a.importDocumentPackage(context.Background(), p, "different", "document_roundtrip_12345"); err == nil {
		t.Fatal("reused operation accepted different file")
	}
}
func mapIDs(pages []documentPackagePage) map[string]string {
	out := map[string]string{}
	for _, p := range pages {
		out[p.ID] = p.ID
	}
	return out
}
func assetIDs(assets []documentPackageAsset) map[string]string {
	out := map[string]string{}
	for _, a := range assets {
		out[a.ID] = a.ID
	}
	return out
}

func TestDocumentPackageRollbackAndCancelledImport(t *testing.T) {
	for _, failure := range []string{"pages", "page_assets", "app_state", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			a := setupTestContext(t)
			defer a.DB.Close()
			_, p := documentFixture(t, a)
			counts := map[string]int{}
			for _, table := range []string{"pages", "page_assets", "app_state"} {
				var countsValue int
				if err := a.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&countsValue); err != nil {
					t.Fatal(err)
				}
				counts[table] = countsValue
			}
			if failure != "cancel" {
				if _, err := a.DB.Exec("CREATE TRIGGER fail_document_import BEFORE INSERT ON " + failure + " BEGIN SELECT RAISE(ABORT,'synthetic failure'); END"); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancel" {
				cancel()
			}
			if _, err := a.importDocumentPackage(ctx, p, "digest", "document_rollback_12345"); err == nil {
				t.Fatal("failure committed")
			}
			for table, want := range counts {
				var count int
				if err := a.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != want {
					t.Fatal("partial import", table, count, want, err)
				}
			}
		})
	}
}

func TestDocumentPackageRejectsInvalidDependenciesAndLimits(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	_, p := documentFixture(t, a)
	raw, _ := json.Marshal(p)
	for _, change := range []func(*documentPackage){
		func(p *documentPackage) { p.Pages = append(p.Pages, p.Pages[0]) },
		func(p *documentPackage) { p.Pages[0].ParentID = p.Pages[0].ID },
		func(p *documentPackage) { p.Assets[0].SHA256 = "broken" },
		func(p *documentPackage) { p.Assets[0].Name = "../escape.png" },
		func(p *documentPackage) {
			p.Pages[0].Content = `[{"type":"image","props":{"url":"https://example.com/pixel.png"}}]`
		},
		func(p *documentPackage) {
			p.Pages[0].Content = `[{"type":"paragraph","content":[{"type":"link","href":"/?page=outside"}]}]`
		},
		func(p *documentPackage) { p.Pages[0].Content = "legacy text" },
		func(p *documentPackage) { p.Pages[0].Content = strings.Repeat("x", (2<<20)+1) },
		func(p *documentPackage) { p.Version = 2 },
	} {
		var candidate documentPackage
		json.Unmarshal(raw, &candidate)
		change(&candidate)
		if err := validateDocumentPackage(candidate); err == nil {
			t.Fatal("invalid package accepted")
		}
	}
	for _, bad := range []string{string(raw) + `{}`, strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(raw), `"version":1`, `"version":1,"unknown":true`, 1)} {
		if _, err := parseDocumentPackage([]byte(bad)); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
	// Covers/linked database/record trees must never be silently downgraded.
	for _, column := range []string{"cover", "linked_database_id"} {
		if _, err := a.DB.Exec("UPDATE pages SET "+column+"='unsupported' WHERE id=?", p.RootID); err != nil {
			t.Fatal(err)
		}
		if _, err := a.exportDocumentPackage(context.Background(), p.RootID); err == nil {
			t.Fatal("unsupported page exported", column)
		}
		a.DB.Exec("UPDATE pages SET "+column+"=NULL WHERE id=?", p.RootID)
	}
}
func TestDocumentPackageHTTPPreviewDigestAndNoPayloadLeak(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	_, p := documentFixture(t, a)
	raw, _ := json.Marshal(p)
	response := httptest.NewRecorder()
	a.handleDocumentPackage(response, httptest.NewRequest("POST", "/api/document-package?preview=1", strings.NewReader(string(raw))))
	if response.Code != 200 || strings.Contains(response.Body.String(), "preserved offline bytes") || strings.Contains(response.Body.String(), `"data":`) {
		t.Fatal("preview leaked bytes or failed", response.Code, response.Body.String())
	}
	var preview documentPackagePreview
	json.Unmarshal(response.Body.Bytes(), &preview)
	response = httptest.NewRecorder()
	a.handleDocumentPackage(response, httptest.NewRequest("POST", "/api/document-package?digest=wrong&operationKey=document_http_123456", strings.NewReader(string(raw))))
	if response.Code != 409 {
		t.Fatal("stale package accepted", response.Code)
	}
	response = httptest.NewRecorder()
	a.handleDocumentPackage(response, httptest.NewRequest("POST", "/api/document-package?digest="+preview.Digest+"&operationKey=document_http_123456", strings.NewReader(string(raw))))
	if response.Code != 201 {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestDocumentPackageCollectsOnlyStructuredReferences(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	source, p := documentFixture(t, a)
	// Exporting a child includes its exact referenced files, including shared
	// root-owned bytes, but not an unreferenced PDF or an asset URL in plain text.
	textID := assetIDForName(t, a, "unused.pdf")
	child := source.Pages["Page.md"]
	var content string
	if err := a.DB.QueryRow("SELECT content FROM pages WHERE id=?", child).Scan(&content); err != nil {
		t.Fatal(err)
	}
	var blocks []any
	json.Unmarshal([]byte(content), &blocks)
	blocks = append(blocks, map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Literal /api/page-assets?id=" + textID, "styles": map[string]any{}}}})
	encoded, _ := json.Marshal(blocks)
	a.DB.Exec("UPDATE pages SET content=? WHERE id=?", string(encoded), child)
	exported, err := a.exportDocumentPackage(context.Background(), child)
	if err != nil || len(exported.Assets) != 2 {
		t.Fatal("unrelated data exported", len(exported.Assets), err)
	}
	for _, asset := range exported.Assets {
		if asset.ID == textID {
			t.Fatal("text URL treated as reference")
		}
	}
	// A child image that points at a text download must fail before import.
	p.Pages[0].Content = `[{"type":"image","props":{"url":"/api/page-assets?id=` + textID + `"}}]`
	if err = validateDocumentPackage(p); err == nil {
		t.Fatal("text download accepted as image")
	}
}

func TestDocumentPackageLargerPreviewUsesRouteLimit(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	p := documentPackage{Format: "reverie-documents", Version: 1, RootID: "large-root"}
	for i, id := range []string{"large-root", "large-child", "large-other"} {
		content, _ := json.Marshal([]any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": strings.Repeat("x", 1500000), "styles": map[string]any{}}}}})
		page := documentPackagePage{ID: id, Title: id, Content: string(content)}
		if i > 0 {
			page.ParentID = p.RootID
		}
		p.Pages = append(p.Pages, page)
	}
	raw, _ := json.Marshal(p)
	if len(raw) <= 4<<20 {
		t.Fatal("fixture does not exceed generic request limit")
	}
	response := httptest.NewRecorder()
	handler := localSecurityMiddleware(AppConfig{Port: 8765}, http.HandlerFunc(a.handleDocumentPackage))
	handler.ServeHTTP(response, httptest.NewRequest("POST", "http://127.0.0.1:8765/api/document-package?preview=1", bytes.NewReader(raw)))
	if response.Code != 200 {
		t.Fatal("valid document transfer hit generic request limit", response.Code, response.Body.String())
	}
}
