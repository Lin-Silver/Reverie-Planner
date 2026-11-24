package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func markdownTestZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func markdownTestBlocks(path, text string) markdownBlocks {
	raw, _ := json.Marshal([]any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text, "styles": map[string]any{}}}}})
	return markdownBlocks{Path: path, Content: raw}
}
func TestMarkdownImportAtomicRestartAndReceipt(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	if _, err := a.DB.Exec(`INSERT INTO pages(id,title,content,workspace) VALUES('existing','Parent','untouched',1)`); err != nil {
		t.Fatal(err)
	}
	raw := markdownTestZIP(t, map[string]string{"Parent.md": "# Parent\n[Child](Parent/Child.md)", "Parent/Child.md": "# Child"})
	preview, err := previewMarkdownZIP(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Pages[1].ParentPath != "Parent.md" {
		t.Fatalf("lost parent: %+v", preview)
	}
	parent := markdownBlocks{Path: "Parent.md", Content: json.RawMessage(`[{"type":"paragraph","content":[{"type":"link","href":"Parent/Child.md","content":[{"type":"text","text":"Child","styles":{}}]}]}]`)}
	blocks := []markdownBlocks{parent, markdownTestBlocks("Parent/Child.md", "Child writing")}
	key := "test_operation_12345678"
	result, err := a.importMarkdown(context.Background(), preview, blocks, key)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	var content, parentID string
	if err = a.DB.QueryRow(`SELECT content FROM pages WHERE id='existing'`).Scan(&content); err != nil || content != "untouched" {
		t.Fatal("existing page modified", err)
	}
	if err = a.DB.QueryRow("SELECT parent_id,content FROM pages WHERE id=?", result.Pages["Parent/Child.md"]).Scan(&parentID, &content); err != nil || parentID != result.Pages["Parent.md"] || !strings.Contains(content, "Child writing") {
		t.Fatal("lost content or parent", err)
	}
	if err = a.DB.QueryRow("SELECT content FROM pages WHERE id=?", result.Pages["Parent.md"]).Scan(&content); err != nil || !strings.Contains(content, "/?page="+result.Pages["Parent/Child.md"]) {
		t.Fatal("link not rewritten", content, err)
	}
	a.DB.Close()
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.importMarkdown(context.Background(), preview, blocks, key)
	if err != nil || again.RootID != result.RootID {
		t.Fatal("retry duplicated import", err)
	}
	if err = a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&count); err != nil || count != 4 {
		t.Fatal(count, err)
	}
	// Independently selected ZIP with identical titles adds copies; never overwrites.
	if _, err = a.importMarkdown(context.Background(), preview, blocks, "separate_operation_987654"); err != nil {
		t.Fatal(err)
	}
	if err = a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&count); err != nil || count != 7 {
		t.Fatal(count, err)
	}
	changed := preview
	changed.Digest = "different"
	if _, err = a.importMarkdown(context.Background(), changed, blocks, key); err == nil {
		t.Fatal("reused key with different ZIP")
	}
}
func TestMarkdownImportRollsBackOnStorageFailureAndCancellation(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	preview, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, map[string]string{"A.md": "first", "B.md": "second"}))
	if err != nil {
		t.Fatal(err)
	}
	blocks := []markdownBlocks{markdownTestBlocks("A.md", "first"), markdownTestBlocks("B.md", "second")}
	if _, err = a.DB.Exec(`CREATE TRIGGER abort_markdown_import BEFORE INSERT ON pages WHEN NEW.title='B' BEGIN SELECT RAISE(ABORT,'synthetic storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.importMarkdown(context.Background(), preview, blocks, "rollback_operation_123456"); err == nil {
		t.Fatal("injected failure accepted")
	}
	var pages, receipts int
	a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&pages)
	a.DB.QueryRow("SELECT count(*) FROM app_state WHERE key LIKE 'markdown_import:%'").Scan(&receipts)
	if pages != 0 || receipts != 0 {
		t.Fatal("partial import survived", pages, receipts)
	}
	if _, err = a.DB.Exec("DROP TRIGGER abort_markdown_import"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = a.importMarkdown(ctx, preview, blocks, "cancel_operation_1234567"); err == nil {
		t.Fatal("cancelled import accepted")
	}
	// Receipt insertion is also transactional; failure must roll back pages.
	if _, err = a.DB.Exec(`CREATE TRIGGER abort_import_receipt BEFORE INSERT ON app_state WHEN NEW.key LIKE 'markdown_import:%' BEGIN SELECT RAISE(ABORT,'receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.importMarkdown(context.Background(), preview, blocks, "receipt_operation_123456"); err == nil {
		t.Fatal("receipt failure accepted")
	}
	a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&pages)
	if pages != 0 {
		t.Fatal("pages committed without receipt")
	}
}
func TestMarkdownZIPRejectsUnsafeOrAmbiguousInputs(t *testing.T) {
	cases := []map[string]string{
		{"../escape.md": "a"}, {"/absolute.md": "a"}, {"C:/drive.md": "a"}, {"a\\b.md": "a"}, {"A.md": "a", "a.md": "b"}, {"A.md": "a", "A.markdown": "b"}, {"invalid.md": string([]byte{255})}, {"nul.md": "a\x00b"},
	}
	for i, files := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if _, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, files)); err == nil {
				t.Fatal("unsafe package accepted")
			}
		})
	}
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: "link.md"}
	header.SetMode(os.ModeSymlink | 0777)
	w, _ := zw.CreateHeader(header)
	w.Write([]byte("other"))
	zw.Close()
	if _, err := previewMarkdownZIP(context.Background(), buffer.Bytes()); err == nil {
		t.Fatal("symlink accepted")
	}
	oversized := markdownTestZIP(t, map[string]string{"big.md": strings.Repeat("a", maxMarkdownPageBytes+1)})
	if _, err := previewMarkdownZIP(context.Background(), oversized); err == nil {
		t.Fatal("oversized entry accepted")
	}
	files := map[string]string{}
	for i := 0; i < maxMarkdownPages+1; i++ {
		files[fmt.Sprintf("%d.md", i)] = "x"
	}
	if _, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, files)); err == nil {
		t.Fatal("too many pages accepted")
	}
	p, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, map[string]string{"A.md": "text", "db.csv": "Name\nA", "image.png": "test"}))
	if err != nil || len(p.Blockers) != 2 {
		t.Fatal("unsupported files not reported", p, err)
	}
	p, err = previewMarkdownZIP(context.Background(), markdownTestZIP(t, map[string]string{"猫 0123456789abcdef0123456789abcdef.md": "你好"}))
	if err != nil || p.Pages[0].Title != "猫" {
		t.Fatal("Notion filename title lost", p, err)
	}
}
func TestMarkdownImportRejectsMissingLinksMediaAndConvertedMismatch(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	preview, _ := previewMarkdownZIP(context.Background(), markdownTestZIP(t, map[string]string{"A.md": "text"}))
	for _, content := range []string{
		`[{"type":"paragraph","content":[{"type":"link","href":"missing.md"}]}]`,
		`[{"type":"paragraph","content":[{"type":"link","href":"javascript:alert(1)"}]}]`,
		`[{"type":"image","props":{"url":"https://example.com/image.png"}}]`,
		`[{"type":"paragraph","content":[{"type":"link","href":"A.md#heading"}]}]`,
		`[{"type":"paragraph","content":[{"type":"link","href":"//example.com/a"}]}]`,
		`[{"type":"paragraph","type":"image"}]`,
	} {
		if _, err := a.importMarkdown(context.Background(), preview, []markdownBlocks{{Path: "A.md", Content: json.RawMessage(content)}}, "invalid_operation_123456"); err == nil {
			t.Fatal("invalid content accepted", content)
		}
	}
	if _, err := a.importMarkdown(context.Background(), preview, []markdownBlocks{markdownTestBlocks("Other.md", "bad")}, "mismatch_operation_123456"); err == nil {
		t.Fatal("wrong converted path accepted")
	}
	var count int
	a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&count)
	if count != 0 {
		t.Fatal("failed import changed data")
	}
	// URL-encoded Unicode and spaces resolve against exact package paths.
	content, err := rewriteImportedBlocks(json.RawMessage(`[{"type":"paragraph","content":[{"type":"link","href":"%E7%8C%AB%20page.md"}]}]`), "A.md", map[string]string{"猫 page.md": "target"})
	if err != nil || !strings.Contains(content, "/?page=target") {
		t.Fatal(content, err)
	}
}
func TestMarkdownPreviewEndpointDoesNotMutate(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	request := httptest.NewRequest("POST", "/api/import/markdown-preview", bytes.NewReader(markdownTestZIP(t, map[string]string{"Page.md": "# Safe preview"})))
	response := httptest.NewRecorder()
	a.handleMarkdownImport(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var count int
	a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&count)
	if count != 0 {
		t.Fatal("preview mutated database")
	}
}

func TestMarkdownImportHTTPPreviewDigestAndRetry(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	raw := markdownTestZIP(t, map[string]string{"Safe.md": "# Safe"})
	preview, err := previewMarkdownZIP(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := json.Marshal([]markdownBlocks{markdownTestBlocks("Safe.md", "safe content")})
	request := func(digest, endpoint string) *httptest.ResponseRecorder {
		var buffer bytes.Buffer
		form := multipart.NewWriter(&buffer)
		file, e := form.CreateFormFile("file", "package.zip")
		if e != nil {
			t.Fatal(e)
		}
		file.Write(raw)
		form.WriteField("digest", digest)
		form.WriteField("operationKey", "http_operation_123456789")
		form.WriteField("blocks", string(blocks))
		form.Close()
		req := httptest.NewRequest("POST", endpoint, &buffer)
		req.Header.Set("Content-Type", form.FormDataContentType())
		result := httptest.NewRecorder()
		a.handleMarkdownImport(result, req)
		return result
	}
	if result := request("changed", "/api/import/markdown"); result.Code != 409 {
		t.Fatal(result.Code, result.Body.String())
	}
	if result := request(preview.Digest, "/api/import/markdown?validate=1"); result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	var count int
	a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&count)
	if count != 0 {
		t.Fatal("validation mutated pages")
	}
	first := request(preview.Digest, "/api/import/markdown")
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	next := request(preview.Digest, "/api/import/markdown")
	if next.Code != 201 || first.Body.String() != next.Body.String() {
		t.Fatal("receipt retry failed", next.Code, next.Body.String())
	}
	a.DB.QueryRow("SELECT count(*) FROM pages").Scan(&count)
	if count != 2 {
		t.Fatal("unexpected page count", count)
	}
}

func TestMarkdownImportPreservesUnmatchedFolders(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	ctx := context.Background()
	raw := markdownTestZIP(t, map[string]string{
		"Notes/Topic.md":            "# Topic",
		"Notes/Topic/Deep/Child.md": "# Child",
		"Assets/readme.txt":         "attachment only",
	})
	preview, err := previewMarkdownZIP(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Pages) != 4 {
		t.Fatalf("want two notes and two folders, got %+v", preview.Pages)
	}
	blocks := []markdownBlocks{
		markdownTestBlocks("Notes/Topic.md", "Topic writing"),
		markdownTestBlocks("Notes/Topic/Deep/Child.md", "Child writing"),
	}
	const key = "markdown_folder_test_123"
	forged := append(append([]markdownBlocks{}, blocks...), markdownTestBlocks("Notes/", "forged content"))
	if _, err := a.importMarkdown(ctx, preview, forged, key); err == nil {
		t.Fatal("folder content must not come from the client")
	}
	result, err := a.importMarkdown(ctx, preview, blocks, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []struct {
		path, title, parent, writing string
	}{
		{"Notes/", "Notes", result.RootID, ""},
		{"Notes/Topic.md", "Topic", result.Pages["Notes/"], "Topic writing"},
		{"Notes/Topic/Deep/", "Deep", result.Pages["Notes/Topic.md"], ""},
		{"Notes/Topic/Deep/Child.md", "Child", result.Pages["Notes/Topic/Deep/"], "Child writing"},
	} {
		var title, parent, content string
		err := a.DB.QueryRow("SELECT title,parent_id,content FROM pages WHERE id=?", result.Pages[page.path]).Scan(&title, &parent, &content)
		if err != nil || title != page.title || parent != page.parent || !strings.Contains(content, page.writing) {
			t.Fatalf("unexpected copy of %q: title=%q parent=%q content=%q error=%v", page.path, title, parent, content, err)
		}
		if page.writing == "" && content != `[{"type":"paragraph","content":[]}]` {
			t.Fatalf("folder %q acquired content: %s", page.path, content)
		}
	}
	retry, err := a.importMarkdown(ctx, preview, blocks, key)
	if err != nil || retry.Pages["Notes/"] != result.Pages["Notes/"] || retry.RootID != result.RootID {
		t.Fatal("retry failed to preserve folder IDs", err)
	}
	tooDeep := markdownTestZIP(t, map[string]string{strings.Repeat("x/", maxMarkdownFolders+1) + "Page.md": "text"})
	if _, err := previewMarkdownZIP(ctx, tooDeep); err == nil || !strings.Contains(err.Error(), "generated folder page limit") {
		t.Fatalf("expected folder bound, got %v", err)
	}
}

// Both explicit entries and implicit ancestors must describe one portable tree.
func TestMarkdownZIPRejectsAmbiguousDirectoryTrees(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"implicit case collision", map[string]string{"Notes/A.md": "a", "notes/B.md": "b"}, "case-ambiguous ZIP path"},
		{"nested case collision", map[string]string{"Notes/Deep/A.md": "a", "Notes/deep/B.md": "b"}, "case-ambiguous ZIP path"},
		{"explicit directory case collision", map[string]string{"Notes/": "", "notes/B.md": "b"}, "case-ambiguous ZIP path"},
		{"file ancestor", map[string]string{"Notes.txt": "file", "Notes.txt/B.md": "b"}, "ZIP file/directory conflict"},
		{"attachment ancestor", map[string]string{"Page.md": "text", "assets.txt": "file", "assets.txt/photo.txt": "attachment"}, "ZIP file/directory conflict"},
		{"case-insensitive page stems", map[string]string{"Notes.md": "a", "notes.markdown": "b"}, "ambiguous Markdown page"},
		{"page folder mismatch", map[string]string{"Notes.md": "a", "notes/B.md": "b"}, "case-ambiguous Markdown page/folder"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// ZIP order is unspecified; neither order may permit an ambiguous tree.
			for i := 0; i < 5; i++ {
				_, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, tc.files))
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("expected %q, got %v", tc.want, err)
				}
			}
		})
	}
	// Explicit directories, implied descendants and matching companion pages
	// remain valid, including shared directory prefixes.
	preview, err := previewMarkdownZIP(context.Background(), markdownTestZIP(t, map[string]string{
		"Notes/": "", "Notes.md": "parent", "Notes/A.md": "a", "Notes/Deep/B.md": "b", "Notes/Deep/C.md": "c",
	}))
	if err != nil || len(preview.Pages) != 5 {
		t.Fatalf("valid directory tree rejected: %+v, %v", preview.Pages, err)
	}
	parents := map[string]string{}
	for _, page := range preview.Pages {
		parents[page.Path] = page.ParentPath
	}
	if parents["Notes/A.md"] != "Notes.md" || parents["Notes/Deep/"] != "Notes.md" || parents["Notes/Deep/B.md"] != "Notes/Deep/" || parents["Notes/Deep/C.md"] != "Notes/Deep/" {
		t.Fatalf("valid hierarchy changed: %+v", parents)
	}
}

func TestMarkdownAmbiguousTreeHTTPRejectsWithoutWrites(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	raw := markdownTestZIP(t, map[string]string{"Notes/A.md": "a", "notes/B.md": "b"})
	preview := httptest.NewRecorder()
	a.handleMarkdownImport(preview, httptest.NewRequest("POST", "/api/import/markdown-preview", bytes.NewReader(raw)))
	if preview.Code != 400 || !strings.Contains(preview.Body.String(), "case-ambiguous ZIP path") {
		t.Fatal(preview.Code, preview.Body.String())
	}
	var buffer bytes.Buffer
	form := multipart.NewWriter(&buffer)
	file, err := form.CreateFormFile("file", "ambiguous.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("blocks", "[]"); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("operationKey", "ambiguous_tree_123456"); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/import/markdown", &buffer)
	req.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	a.handleMarkdownImport(response, req)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "case-ambiguous ZIP path") {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, query := range []string{
		"SELECT count(*) FROM pages",
		"SELECT count(*) FROM page_assets",
		"SELECT count(*) FROM app_state WHERE key LIKE 'markdown_import:%'",
	} {
		var count int
		if err := a.DB.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rejected package mutated state: %s: %d, %v", query, count, err)
		}
	}
}
