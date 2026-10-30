package main

import (
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
	"sort"
	"strings"
	"unicode/utf8"
)

const maxDocumentPackageBytes = 48 << 20
const maxDocumentContentBytes = 16 << 20

type documentPackagePage struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	Title    string `json:"title"`
	Icon     string `json:"icon,omitempty"`
	Content  string `json:"content"`
}
type documentPackageAsset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	SHA256      string `json:"sha256"`
	Data        []byte `json:"data"`
}
type documentPackage struct {
	Format  string                 `json:"format"`
	Version int                    `json:"version"`
	RootID  string                 `json:"rootId"`
	Pages   []documentPackagePage  `json:"pages"`
	Assets  []documentPackageAsset `json:"assets"`
}
type documentPackagePreview struct {
	Digest          string                `json:"digest"`
	Pages           []documentPackagePage `json:"pages"`
	Attachments     int                   `json:"attachments"`
	AttachmentBytes int                   `json:"attachmentBytes"`
}

// A package is a document transfer, not a database/history backup. Reject local
// dependencies outside it instead of silently exporting broken references.
func documentReference(raw string, pages, assets map[string]string, image bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid document URL")
	}
	if u.IsAbs() && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") && !image {
		return raw, nil
	}
	if u.Scheme == "mailto" && !image {
		return raw, nil
	}
	if u.Scheme != "" || u.Host != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("unsupported document URL; use a workspace archive for other media")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 1 {
		return "", errors.New("unsupported local reference")
	}
	if u.Path == "/" && !image && len(query["page"]) == 1 {
		if id := pages[query.Get("page")]; id != "" {
			return "/?page=" + url.QueryEscape(id), nil
		}
	}
	if u.Path == "/api/page-assets" && len(query["id"]) == 1 {
		if id := assets[query.Get("id")]; id != "" {
			return "/api/page-assets?id=" + url.QueryEscape(id), nil
		}
	}
	return "", errors.New("reference points outside this document tree or to unsupported media; export a containing tree or use a workspace archive")
}

type documentRewriteOptions struct {
	UsedAssets  map[string]bool
	ImageAssets map[string]bool
}

func rewriteDocumentContent(content string, pages, assets map[string]string, options ...documentRewriteOptions) (string, error) {
	var used, images map[string]bool
	if len(options) > 0 {
		used = options[0].UsedAssets
		images = options[0].ImageAssets
	}
	if len(content) > 2<<20 {
		return "", errors.New("document exceeds 2 MiB")
	}
	if strings.TrimSpace(content) == "" {
		return content, nil
	}
	// Reject legacy non-block content explicitly; a document package never guesses
	// conversion that would change writing on a later editor save.
	if !strings.HasPrefix(strings.TrimSpace(content), "[") {
		return "", errors.New("only editor block documents are supported; use a workspace archive for legacy plain text")
	}
	if err := rejectDuplicateJSONKeys([]byte(content)); err != nil {
		return "", err
	}
	var blocks []any
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&blocks); err != nil || len(blocks) == 0 || len(blocks) > 10000 {
		return "", errors.New("invalid editor blocks")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", errors.New("unexpected editor content")
	}
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		if depth > 64 {
			return errors.New("document nesting limit exceeded")
		}
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case map[string]any:
			kind, _ := node["type"].(string)
			if kind != "" {
				switch kind {
				case "paragraph", "heading", "bulletListItem", "numberedListItem", "checkListItem", "toggleListItem", "codeBlock", "divider", "quote", "table", "tableContent", "tableCell", "text", "link", "image", "file", "audio", "video":
				default:
					return fmt.Errorf("unsupported editor type: %s", kind)
				}
			}
			if kind == "video" || kind == "audio" {
				return errors.New("audio/video transfer is not supported; use a workspace archive")
			}
			if href, exists := node["href"]; exists {
				text, ok := href.(string)
				if !ok {
					return errors.New("invalid link")
				}
				rewritten, err := documentReference(text, pages, assets, false)
				if err != nil {
					return err
				}
				node["href"] = rewritten
				if used != nil && strings.HasPrefix(rewritten, "/api/page-assets?id=") {
					used[strings.TrimPrefix(rewritten, "/api/page-assets?id=")] = true
				}
			}
			if kind == "image" || kind == "file" {
				props, ok := node["props"].(map[string]any)
				if !ok {
					return errors.New("invalid media props")
				}
				text, ok := props["url"].(string)
				if !ok {
					return errors.New("invalid media URL")
				}
				rewritten, err := documentReference(text, pages, assets, true)
				if err != nil {
					return err
				}
				if kind == "image" && images != nil {
					original, _ := url.Parse(text)
					if !images[original.Query().Get("id")] {
						return errors.New("image must reference a raster image attachment")
					}
				}
				props["url"] = rewritten
				if used != nil {
					used[strings.TrimPrefix(rewritten, "/api/page-assets?id=")] = true
				}
				for key := range props {
					switch key {
					case "url", "caption", "name", "previewWidth", "showPreview", "textAlignment", "backgroundColor":
					default:
						return fmt.Errorf("unsupported media property: %s", key)
					}
				}
			}
			for _, child := range node {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, block := range blocks {
		item, ok := block.(map[string]any)
		if !ok {
			return "", errors.New("invalid editor block")
		}
		kind, _ := item["type"].(string)
		switch kind {
		case "paragraph", "heading", "bulletListItem", "numberedListItem", "checkListItem", "toggleListItem", "codeBlock", "divider", "quote", "table", "image", "file":
		default:
			return "", errors.New("unsupported document block type")
		}
		if children, exists := item["children"]; exists && children != nil {
			if _, ok := children.([]any); !ok {
				return "", errors.New("invalid child blocks")
			}
		}
		if item["type"] == nil {
			return "", errors.New("invalid editor block")
		}
	}
	if err := visit(blocks, 0); err != nil {
		return "", err
	}
	raw, err := json.Marshal(blocks)
	return string(raw), err
}

func validateDocumentPackage(p documentPackage) error {
	if p.Format != "reverie-documents" || p.Version != 1 || len(p.Pages) == 0 || len(p.Pages) > 100 || len(p.Assets) > 500 {
		return errors.New("unsupported document package or item limit exceeded")
	}
	pages, assets := map[string]string{}, map[string]string{}
	parents := map[string]string{}
	total := 0
	for _, page := range p.Pages {
		if !utf8.ValidString(page.Title) || !utf8.ValidString(page.Icon) || !utf8.ValidString(page.Content) || !archiveAttachmentID.MatchString(page.ID) || pages[page.ID] != "" || len(page.Title) > 4000 || len(page.Icon) > 256 {
			return errors.New("invalid or duplicate document")
		}
		pages[page.ID] = page.ID
		parents[page.ID] = page.ParentID
		total += len(page.Content)
	}
	if total > maxDocumentContentBytes || pages[p.RootID] == "" || parents[p.RootID] != "" {
		return errors.New("invalid document root or content limit exceeded")
	}
	for _, page := range p.Pages {
		seen := map[string]bool{}
		id := page.ID
		for id != p.RootID {
			if seen[id] || pages[id] == "" || parents[id] == "" {
				return errors.New("document parents must form one tree")
			}
			seen[id] = true
			id = parents[id]
		}
	}
	images := map[string]bool{}
	total = 0
	for _, asset := range p.Assets {
		checked, ok := inspectMarkdownAsset(asset.Name, asset.Data)
		if !utf8.ValidString(asset.Name) || !archiveAttachmentID.MatchString(asset.ID) || assets[asset.ID] != "" || strings.ContainsAny(asset.Name, "/\\\x00\r\n") || len(asset.Name) > 500 || !ok || len(asset.Data) > maxMarkdownAssetBytes || asset.SHA256 != checked.SHA256 || asset.ContentType != checked.ContentType {
			return errors.New("invalid attachment, format or checksum")
		}
		assets[asset.ID] = asset.ID
		images[asset.ID] = strings.HasPrefix(asset.ContentType, "image/")
		total += len(asset.Data)
	}
	if total > 16<<20 {
		return errors.New("attachments exceed 16 MiB")
	}
	for _, page := range p.Pages {
		if _, err := rewriteDocumentContent(page.Content, pages, assets, documentRewriteOptions{ImageAssets: images}); err != nil {
			return fmt.Errorf("%s: %w", page.Title, err)
		}
	}
	return nil
}
func parseDocumentPackage(raw []byte) (documentPackage, error) {
	var p documentPackage
	if !utf8.Valid(raw) {
		return p, errors.New("document package must be UTF-8")
	}
	if len(raw) > maxDocumentPackageBytes {
		return p, errors.New("document package exceeds 48 MiB")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return p, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return p, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return p, errors.New("unexpected package content")
	}
	return p, validateDocumentPackage(p)
}

func (a *AppContext) exportDocumentPackage(ctx context.Context, root string) (documentPackage, error) {
	p := documentPackage{Format: "reverie-documents", Version: 1, RootID: root, Pages: []documentPackagePage{}, Assets: []documentPackageAsset{}}
	tx, err := a.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE tree(id) AS (SELECT id FROM pages WHERE id=? UNION SELECT p.id FROM pages p JOIN tree t ON p.parent_id=t.id)
 SELECT p.id,COALESCE(p.parent_id,''),p.title,COALESCE(p.icon,''),COALESCE(p.content,''),COALESCE(p.cover,''),
 COALESCE(p.linked_database_id,''),EXISTS(SELECT 1 FROM databases WHERE page_id=p.id) OR EXISTS(SELECT 1 FROM db_records WHERE page_id=p.id)
 FROM pages p JOIN tree t ON t.id=p.id ORDER BY p.id LIMIT 101`, root)
	if err != nil {
		return p, err
	}
	ids := map[string]bool{}
	total := 0
	for rows.Next() {
		var page documentPackagePage
		var cover, linked string
		var owned bool
		if err = rows.Scan(&page.ID, &page.ParentID, &page.Title, &page.Icon, &page.Content, &cover, &linked, &owned); err != nil {
			break
		}
		if cover != "" || linked != "" || owned {
			err = errors.New("tree includes covers or database-owned/linked pages; use a workspace archive")
			break
		}
		if page.ID == root {
			page.ParentID = ""
		}
		total += len(page.Content)
		if total > maxDocumentContentBytes {
			err = errors.New("document content exceeds 16 MiB")
			break
		}
		p.Pages = append(p.Pages, page)
		ids[page.ID] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return p, err
	}
	if len(p.Pages) == 0 || len(p.Pages) > 100 {
		return p, errors.New("document tree must contain 1–100 pages")
	}
	// Only fetch bounded bytes owned by the selected tree or explicitly referenced
	// by it. Never include unrelated workspace assets or provider configuration.
	rows, err = tx.QueryContext(ctx, "SELECT id,root_id FROM page_assets ORDER BY id")
	if err != nil {
		return p, err
	}
	ownedAssets := map[string]bool{}
	allAssets := map[string]string{}
	for rows.Next() {
		var id, owner string
		if err = rows.Scan(&id, &owner); err != nil {
			break
		}
		allAssets[id] = id
		ownedAssets[id] = ids[owner]
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return p, err
	}
	pageIDs := map[string]string{}
	for id := range ids {
		pageIDs[id] = id
	}
	referenced := map[string]bool{}
	for _, page := range p.Pages {
		if _, err = rewriteDocumentContent(page.Content, pageIDs, allAssets, documentRewriteOptions{UsedAssets: referenced}); err != nil {
			return p, err
		}
	}
	assetIDs := []string{}
	for id := range allAssets {
		if ownedAssets[id] || referenced[id] {
			assetIDs = append(assetIDs, id)
		}
	}
	sort.Strings(assetIDs)
	if len(assetIDs) > 500 {
		return p, errors.New("too many document attachments")
	}
	total = 0
	for _, id := range assetIDs {
		var asset documentPackageAsset
		asset.ID = id
		err = tx.QueryRowContext(ctx, `SELECT a.original_name,a.content_type,a.sha256,a.data FROM page_assets a JOIN pages owner ON owner.id=a.root_id WHERE a.id=?`, id).Scan(&asset.Name, &asset.ContentType, &asset.SHA256, &asset.Data)
		if err != nil {
			return p, errors.New("document attachment unavailable")
		}
		total += len(asset.Data)
		if total > 16<<20 {
			return p, errors.New("attachments exceed 16 MiB")
		}
		p.Assets = append(p.Assets, asset)
	}
	if err = validateDocumentPackage(p); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

func (a *AppContext) importDocumentPackage(ctx context.Context, p documentPackage, digest, key string) (markdownImportResult, error) {
	result := markdownImportResult{Digest: digest, Pages: map[string]string{}}
	if err := validateDocumentPackage(p); err != nil {
		return result, err
	}
	if !importKeyPattern.MatchString(key) {
		return result, errors.New("valid operation key required")
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	receipt := "document_import:" + key
	var saved string
	err = tx.QueryRowContext(ctx, "SELECT value FROM app_state WHERE key=?", receipt).Scan(&saved)
	if err == nil {
		if err = json.Unmarshal([]byte(saved), &result); err != nil {
			return result, err
		}
		if result.Digest != digest {
			return result, errors.New("operation key belongs to another package")
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	result.RootID = newID("page")
	for _, page := range p.Pages {
		result.Pages[page.ID] = newID("page")
	}
	assets := map[string]string{}
	for _, asset := range p.Assets {
		assets[asset.ID] = newID("pageasset")
	}
	now := timeNowUTC()
	insert := func(id, parent, title, icon, content string) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO pages(id,parent_id,title,icon,content,workspace,created_by,created_at,updated_at,version) VALUES(?,?,?,?,?,1,'Import',?,?,1)`, id, nullStr(parent), title, nullStr(icon), content, now, now)
		return e
	}
	if err = insert(result.RootID, "", "Imported documents", "", `[{"type":"paragraph","content":[]}]`); err != nil {
		return result, err
	}
	for _, page := range p.Pages {
		parent := result.RootID
		if page.ParentID != "" {
			parent = result.Pages[page.ParentID]
		}
		content, e := rewriteDocumentContent(page.Content, result.Pages, assets)
		if e != nil {
			return result, e
		}
		if err = insert(result.Pages[page.ID], parent, page.Title, page.Icon, content); err != nil {
			return result, err
		}
	}
	for _, asset := range p.Assets {
		if _, err = tx.ExecContext(ctx, `INSERT INTO page_assets(id,root_id,original_name,content_type,size_bytes,sha256,data) VALUES(?,?,?,?,?,?,?)`, assets[asset.ID], result.Pages[p.RootID], asset.Name, asset.ContentType, len(asset.Data), asset.SHA256, asset.Data); err != nil {
			return result, err
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO app_state(key,value) VALUES(?,?)", receipt, string(encoded)); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func (a *AppContext) handleDocumentPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		p, err := a.exportDocumentPackage(r.Context(), r.URL.Query().Get("rootId"))
		if err != nil {
			writeJSONError(w, 400, err.Error())
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="Reverie-documents.reverie.json"`)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, p)
		return
	}
	if r.Method != "POST" {
		writeJSONError(w, 405, "method not allowed")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxDocumentPackageBytes+1))
	if err != nil {
		writeJSONError(w, 400, "unable to read document package")
		return
	}
	p, err := parseDocumentPackage(raw)
	if err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	if r.URL.Query().Get("preview") == "1" {
		size := 0
		for _, asset := range p.Assets {
			size += len(asset.Data)
		}
		writeJSON(w, 200, documentPackagePreview{Digest: digest, Pages: p.Pages, Attachments: len(p.Assets), AttachmentBytes: size})
		return
	}
	if r.URL.Query().Get("digest") != digest {
		writeJSONError(w, 409, "package changed; preview again")
		return
	}
	result, err := a.importDocumentPackage(r.Context(), p, digest, r.URL.Query().Get("operationKey"))
	if err != nil {
		writeJSONError(w, 400, "Import was not applied: "+err.Error())
		return
	}
	writeJSON(w, 201, result)
}
