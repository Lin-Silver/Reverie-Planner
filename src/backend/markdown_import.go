package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxMarkdownZIPBytes = 16 << 20
const maxMarkdownExpandedBytes = 16 << 20
const maxMarkdownPageBytes = 1 << 20
const maxMarkdownPages = 100
const maxMarkdownEntries = 500

var notionTitleSuffix = regexp.MustCompile(` [a-fA-F0-9]{32}$`)
var importKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`)

type markdownSource struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	ParentPath string `json:"parentPath,omitempty"`
	Markdown   string `json:"markdown"`
}
type markdownPreview struct {
	Digest   string           `json:"digest"`
	Pages    []markdownSource `json:"pages"`
	Assets   []markdownAsset  `json:"assets"`
	Blockers []string         `json:"blockers"`
}
type markdownBlocks struct {
	Path    string          `json:"path"`
	Content json.RawMessage `json:"content"`
}
type markdownImportResult struct {
	Digest string            `json:"digest"`
	RootID string            `json:"rootId"`
	Pages  map[string]string `json:"pages"`
}

// ZIP bytes are never extracted. Every entry, including ignored directories, is
// validated. Supported assets remain in memory until the single SQLite commit.
func previewMarkdownZIP(ctx context.Context, raw []byte) (markdownPreview, error) {
	sum := sha256.Sum256(raw)
	out := markdownPreview{Digest: hex.EncodeToString(sum[:]), Pages: []markdownSource{}, Blockers: []string{}, Assets: []markdownAsset{}}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return out, errors.New("not a valid Markdown ZIP")
	}
	if len(zr.File) > maxMarkdownEntries {
		return out, errors.New("ZIP exceeds 500 entry limit")
	}
	seen := map[string]bool{}
	var expanded uint64
	for _, file := range zr.File {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		name := strings.TrimSuffix(file.Name, "/")
		if name == "" || len(name) > 500 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00\r\n") || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return out, fmt.Errorf("unsafe ZIP path: %q", file.Name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return out, fmt.Errorf("duplicate or case-ambiguous ZIP path: %s", name)
		}
		seen[key] = true
		if file.Mode() & ^(file.Mode().Perm()) != 0 && !file.FileInfo().IsDir() {
			return out, fmt.Errorf("non-regular ZIP entry: %s", name)
		}
		if file.Flags&1 != 0 {
			return out, errors.New("encrypted ZIP is unsupported")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		limit := maxMarkdownPageBytes
		if ext := strings.ToLower(path.Ext(name)); ext != ".md" && ext != ".markdown" {
			limit = maxMarkdownAssetBytes
		}
		if file.UncompressedSize64 > uint64(limit) {
			return out, fmt.Errorf("entry exceeds %d byte limit: %s", limit, name)
		}
		expanded += file.UncompressedSize64
		if expanded > maxMarkdownExpandedBytes {
			return out, errors.New("ZIP exceeds 16 MiB expanded limit")
		}
		reader, e := file.Open()
		if e != nil {
			return out, e
		}
		data, e := io.ReadAll(io.LimitReader(archiveContextReader{ctx, reader}, int64(limit)+1))
		reader.Close()
		if e != nil {
			return out, fmt.Errorf("cannot read ZIP entry %s: %w", name, e)
		}
		if len(data) > limit {
			return out, errors.New("entry exceeds expanded limit")
		}
		ext := strings.ToLower(path.Ext(name))
		if ext != ".md" && ext != ".markdown" {
			asset, supported := inspectMarkdownAsset(name, data)
			if !supported {
				out.Blockers = append(out.Blockers, "Unsupported file: "+name+". Only PNG/JPEG/GIF/WebP images, PDF, TXT and Office attachments are supported; CSV databases are not yet supported.")
			} else {
				out.Assets = append(out.Assets, asset)
			}
			continue
		}
		if !utf8.Valid(data) || bytes.Contains(data, []byte{0}) {
			return out, fmt.Errorf("Markdown must be UTF-8 text: %s", name)
		}
		title := notionTitleSuffix.ReplaceAllString(strings.TrimSuffix(path.Base(name), path.Ext(name)), "")
		if strings.TrimSpace(title) == "" {
			title = "Untitled"
		}
		out.Pages = append(out.Pages, markdownSource{Path: name, Title: title, Markdown: strings.TrimPrefix(string(data), "\ufeff")})
		if len(out.Pages) > maxMarkdownPages {
			return out, errors.New("ZIP exceeds 100 Markdown page limit")
		}
	}
	if len(out.Pages) == 0 {
		return out, errors.New("ZIP contains no Markdown pages")
	}
	sort.Slice(out.Pages, func(i, j int) bool { return out.Pages[i].Path < out.Pages[j].Path })
	sort.Strings(out.Blockers)
	sort.Slice(out.Assets, func(i, j int) bool { return out.Assets[i].Path < out.Assets[j].Path })
	// A directory corresponds to its sibling page (the Notion Markdown layout).
	// Otherwise use the nearest explicit ancestor page; the source path is always
	// shown in preview. No guessed/synthetic page content is generated.
	stems := map[string]string{}
	for _, p := range out.Pages {
		stem := strings.TrimSuffix(p.Path, path.Ext(p.Path))
		if _, ok := stems[stem]; ok {
			return out, fmt.Errorf("ambiguous Markdown page: %s", stem)
		}
		stems[stem] = p.Path
	}
	for i, p := range out.Pages {
		for dir := path.Dir(p.Path); dir != "."; dir = path.Dir(dir) {
			if parent, ok := stems[dir]; ok {
				out.Pages[i].ParentPath = parent
				break
			}
		}
	}
	return out, nil
}

// Only stored editor blocks are rewritten. No raw HTML is rendered, remote
// content is never fetched, and dangling local links fail the whole transaction.
func rewriteImportedBlocks(raw json.RawMessage, source string, ids map[string]string, assets ...map[string]string) (string, error) {
	if len(raw) > 2<<20 {
		return "", errors.New("converted page exceeds 2 MiB")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return "", err
	}
	var blocks []any
	if err := json.Unmarshal(raw, &blocks); err != nil || len(blocks) == 0 || len(blocks) > 10000 {
		return "", errors.New("converted page must contain editor blocks")
	}
	for _, block := range blocks {
		item, ok := block.(map[string]any)
		if !ok {
			return "", errors.New("invalid editor block")
		}
		kind, ok := item["type"].(string)
		if !ok || kind == "" {
			return "", errors.New("editor block type required")
		}
	}
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		if depth > 64 {
			return errors.New("editor blocks are too deeply nested")
		}
		switch node := value.(type) {
		case []any:
			for _, v := range node {
				if err := visit(v, depth+1); err != nil {
					return err
				}
			}
		case map[string]any:
			if kind, _ := node["type"].(string); kind == "image" || kind == "video" || kind == "audio" || kind == "file" {
				if kind != "image" && kind != "file" {
					return errors.New("embedded audio/video is not supported")
				}
				props, ok := node["props"].(map[string]any)
				if !ok {
					return errors.New("embedded media requires local asset props")
				}
				link, ok := props["url"].(string)
				if !ok {
					return errors.New("embedded media requires a local asset URL")
				}
				target, err := markdownLocalTarget(source, link)
				if err != nil {
					return err
				}
				assetURL := ""
				if len(assets) > 0 {
					assetURL = assets[0][target]
				}
				if assetURL == "" {
					return fmt.Errorf("missing local asset target: %s", target)
				}
				if kind == "image" && !markdownImageExtension(target) {
					return errors.New("image block must reference a supported image")
				}
				props["url"] = assetURL
				// A client must not smuggle automatic external requests via extra media props.
				for key := range props {
					if key != "url" && key != "caption" && key != "name" && key != "previewWidth" && key != "showPreview" && key != "textAlignment" && key != "backgroundColor" {
						delete(props, key)
					}
				}
			}
			if href, ok := node["href"]; ok {
				link, ok := href.(string)
				if !ok {
					return errors.New("invalid link")
				}
				u, err := url.Parse(link)
				if err != nil {
					return errors.New("invalid link URL")
				}
				if u.IsAbs() {
					if u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto" {
						return errors.New("unsupported link scheme")
					}
				} else {
					if u.Host != "" || u.RawQuery != "" || u.Path == "" || strings.HasPrefix(u.Path, "/") || strings.Contains(u.Path, "\\") {
						return fmt.Errorf("unsupported local link: %s", link)
					}
					if u.Fragment != "" {
						return fmt.Errorf("local heading anchors are not yet supported: %s", link)
					}
					target := path.Clean(path.Join(path.Dir(source), u.Path))
					id, exists := ids[target]
					if exists {
						node["href"] = "/?page=" + url.QueryEscape(id)
					} else if len(assets) > 0 && assets[0][target] != "" {
						node["href"] = assets[0][target]
					} else {
						return fmt.Errorf("missing local link target: %s", target)
					}
				}
			}
			for _, v := range node {
				if err := visit(v, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(blocks, 0); err != nil {
		return "", err
	}
	result, err := json.Marshal(blocks)
	return string(result), err
}

func convertMarkdownPages(preview markdownPreview, converted []markdownBlocks, ids map[string]string, assets ...map[string]string) (map[string]string, error) {
	if len(preview.Blockers) > 0 {
		return nil, errors.New("unsupported package files must be resolved before importing")
	}
	if len(converted) != len(preview.Pages) {
		return nil, errors.New("converted page count does not match preview")
	}
	contents := map[string]json.RawMessage{}
	for _, p := range converted {
		if _, exists := contents[p.Path]; exists {
			return nil, errors.New("duplicate converted page")
		}
		contents[p.Path] = p.Content
	}
	documents := map[string]string{}
	for _, p := range preview.Pages {
		content, ok := contents[p.Path]
		if !ok {
			return nil, fmt.Errorf("missing converted page: %s", p.Path)
		}
		value, err := rewriteImportedBlocks(content, p.Path, ids, assets...)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Path, err)
		}
		documents[p.Path] = value
	}
	return documents, nil
}

func (a *AppContext) importMarkdown(ctx context.Context, preview markdownPreview, converted []markdownBlocks, key string) (markdownImportResult, error) {
	result := markdownImportResult{Digest: preview.Digest, Pages: map[string]string{}}
	if len(preview.Blockers) > 0 {
		return result, errors.New("unsupported package files must be resolved before importing")
	}
	if !importKeyPattern.MatchString(key) {
		return result, errors.New("valid import operation key required")
	}
	// The receipt is in the same transaction as all pages. Retrying a completed
	// operation after a lost response returns the same IDs rather than copies.
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	receiptKey := "markdown_import:" + key
	var saved string
	err = tx.QueryRowContext(ctx, "SELECT value FROM app_state WHERE key=?", receiptKey).Scan(&saved)
	if err == nil {
		if e := json.Unmarshal([]byte(saved), &result); e != nil {
			return result, e
		}
		if result.Digest != preview.Digest {
			return result, errors.New("import operation key belongs to a different ZIP")
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	result.RootID = newID("page")
	for _, p := range preview.Pages {
		result.Pages[p.Path] = newID("page")
	}
	assetURLs := map[string]string{}
	for _, asset := range preview.Assets {
		assetURLs[asset.Path] = "/api/page-assets?id=" + newID("pageasset")
	}
	documents, err := convertMarkdownPages(preview, converted, result.Pages, assetURLs)
	if err != nil {
		return result, err
	}
	insert := func(id, parent, title, content string) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO pages(id,parent_id,title,content,workspace,created_by,created_at,updated_at,version) VALUES(?,?,?,?,1,'Import',?,?,1)`, id, nullStr(parent), title, content, timeNowUTC(), timeNowUTC())
		return e
	}
	rootContent := `[{"type":"paragraph","content":[]}]`
	if len(preview.Assets) > 0 {
		index := []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Imported attachments", "styles": map[string]any{"bold": true}}}}}
		for _, asset := range preview.Assets {
			index = append(index, map[string]any{"type": "bulletListItem", "content": []any{map[string]any{"type": "link", "href": assetURLs[asset.Path], "content": []any{map[string]any{"type": "text", "text": asset.Path, "styles": map[string]any{}}}}}})
		}
		encoded, e := json.Marshal(index)
		if e != nil {
			return result, e
		}
		rootContent = string(encoded)
	}
	if err = insert(result.RootID, "", "Imported Markdown", rootContent); err != nil {
		return result, err
	}
	for _, p := range preview.Pages {
		parent := result.RootID
		if p.ParentPath != "" {
			parent = result.Pages[p.ParentPath]
		}
		if err = insert(result.Pages[p.Path], parent, p.Title, documents[p.Path]); err != nil {
			return result, err
		}
	}
	// Bytes, ownership and pages share the same transaction. No published files
	// can survive a failed commit, and retries reuse the original receipt.
	for _, asset := range preview.Assets {
		id := strings.TrimPrefix(assetURLs[asset.Path], "/api/page-assets?id=")
		if _, err = tx.ExecContext(ctx, `INSERT INTO page_assets(id,root_id,original_name,content_type,size_bytes,sha256,data) VALUES(?,?,?,?,?,?,?)`, id, result.RootID, path.Base(asset.Path), asset.ContentType, asset.Size, asset.SHA256, asset.Data); err != nil {
			return result, err
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO app_state(key,value) VALUES(?,?)", receiptKey, string(encoded)); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (a *AppContext) handleMarkdownImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSONError(w, 405, "method not allowed")
		return
	}
	var raw []byte
	var converted []markdownBlocks
	var err error
	previewOnly := r.URL.Path == "/api/import/markdown-preview"
	if previewOnly {
		raw, err = io.ReadAll(io.LimitReader(r.Body, maxMarkdownZIPBytes+1))
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 40<<20)
		err = r.ParseMultipartForm(20 << 20)
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		if err == nil {
			file, _, e := r.FormFile("file")
			err = e
			if err == nil {
				raw, err = io.ReadAll(io.LimitReader(file, maxMarkdownZIPBytes+1))
				file.Close()
			}
		}
		if err == nil {
			blocks := r.FormValue("blocks")
			if len(blocks) > 20<<20 {
				err = errors.New("converted pages exceed 20 MiB")
			} else if e := rejectDuplicateJSONKeys([]byte(blocks)); e != nil {
				err = e
			} else {
				err = json.Unmarshal([]byte(blocks), &converted)
			}
		}
	}
	if err != nil {
		writeJSONError(w, 400, "Unable to read import package: "+err.Error())
		return
	}
	if len(raw) > maxMarkdownZIPBytes {
		writeJSONError(w, 413, "ZIP exceeds 16 MiB compressed limit")
		return
	}
	preview, err := previewMarkdownZIP(r.Context(), raw)
	if err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	if previewOnly {
		writeJSON(w, 200, preview)
		return
	}
	if r.FormValue("digest") != preview.Digest {
		writeJSONError(w, 409, "ZIP changed since preview; preview it again")
		return
	}
	if r.URL.Query().Get("validate") == "1" {
		ids := map[string]string{}
		for _, p := range preview.Pages {
			ids[p.Path] = "preview"
		}
		assets := map[string]string{}
		for _, asset := range preview.Assets {
			assets[asset.Path] = "/api/page-assets?id=preview"
		}
		if _, err := convertMarkdownPages(preview, converted, ids, assets); err != nil {
			writeJSONError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"valid": true})
		return
	}
	result, err := a.importMarkdown(r.Context(), preview, converted, r.FormValue("operationKey"))
	if err != nil {
		writeJSONError(w, 400, "Import was not applied: "+err.Error())
		return
	}
	writeJSON(w, 201, result)
}
