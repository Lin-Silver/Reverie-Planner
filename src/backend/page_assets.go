package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const maxMarkdownAssetBytes = 8 << 20

// Bounded imported assets live inside SQLite, rather than publishing files
// outside the page transaction. The import root owns shared and unreferenced
// assets; retain them through page trash and history just like the documents.
const pageAssetsSQL = `CREATE TABLE page_assets (
 id TEXT PRIMARY KEY, root_id TEXT NOT NULL, original_name TEXT NOT NULL,
 content_type TEXT NOT NULL, size_bytes INTEGER NOT NULL CHECK(size_bytes>=0 AND size_bytes<=8388608),
 sha256 TEXT NOT NULL, data BLOB NOT NULL CHECK(length(data)=size_bytes)
)`

type markdownAsset struct {
	Path        string `json:"path"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"contentType"`
	Data        []byte `json:"-"`
}

func markdownImageExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}
func inspectMarkdownAsset(name string, data []byte) (markdownAsset, bool) {
	ext := strings.ToLower(path.Ext(name))
	contentType := http.DetectContentType(data)
	allowed := false
	if markdownImageExtension(name) {
		want := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}[ext]
		allowed = contentType == want
	} else {
		switch ext {
		case ".pdf", ".txt", ".docx", ".xlsx", ".pptx":
			allowed = true
		}
		// Non-image bytes always download, regardless of the declared extension.
		contentType = "application/octet-stream"
	}
	sum := sha256.Sum256(data)
	return markdownAsset{Path: name, Size: len(data), SHA256: hex.EncodeToString(sum[:]), ContentType: contentType, Data: data}, allowed
}
func markdownLocalTarget(source, link string) (string, error) {
	u, err := url.Parse(link)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || strings.HasPrefix(u.Path, "/") || strings.ContainsAny(u.Path, "\\\x00") {
		return "", fmt.Errorf("unsupported local asset link: %s", link)
	}
	target := path.Clean(path.Join(path.Dir(source), u.Path))
	if target == ".." || strings.HasPrefix(target, "../") {
		return "", errors.New("asset link escapes package")
	}
	return target, nil
}
func migratePageAssets(db *sql.DB) error {
	var applied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=12").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return verifyPageAssetsSchema(db)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(pageAssetsSQL); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES(12,CURRENT_TIMESTAMP)"); err != nil {
		return err
	}
	return tx.Commit()
}
func verifyPageAssetsSchema(db *sql.DB) error {
	var statement string
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='page_assets'").Scan(&statement); err != nil {
		return err
	}
	if statement != pageAssetsSQL {
		return errors.New("page asset schema changed; back up and inspect before repair")
	}
	return nil
}

// Called on the locked source before archive creation and on staged restoration.
// Reading one bounded BLOB at a time avoids copying all imported packages at once.
func verifyPageAssetsData(ctx context.Context, db *sql.DB) (int, error) {
	var applied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=12").Scan(&applied); err != nil {
		return 0, err
	}
	if applied == 0 {
		return 0, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT a.original_name,a.content_type,a.size_bytes,a.sha256,a.data,
 EXISTS(SELECT 1 FROM pages WHERE id=a.root_id) OR EXISTS(SELECT 1 FROM page_trash WHERE id=a.root_id)
 FROM page_assets a`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name, kind, hash string
		var size int
		var data []byte
		var owned bool
		if err = rows.Scan(&name, &kind, &size, &hash, &data, &owned); err != nil {
			return 0, err
		}
		asset, ok := inspectMarkdownAsset(name, data)
		if !owned || !ok || len(data) > maxMarkdownAssetBytes || size != len(data) || hash != asset.SHA256 || kind != asset.ContentType {
			return 0, errors.New("page attachment checksum, format or ownership failed validation")
		}
		count++
	}
	return count, rows.Err()
}
func (a *AppContext) handlePageAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		writeJSONError(w, 405, "method not allowed")
		return
	}
	var name, kind, hash string
	var data []byte
	var size int
	err := a.DB.QueryRowContext(r.Context(), `SELECT a.original_name,a.content_type,a.size_bytes,a.sha256,a.data
 FROM page_assets a JOIN pages p ON p.id=a.root_id WHERE a.id=?`, r.URL.Query().Get("id")).Scan(&name, &kind, &size, &hash, &data)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, 404, "page attachment not found")
		return
	}
	if err != nil {
		writeJSONError(w, 500, "unable to read page attachment")
		return
	}
	asset, ok := inspectMarkdownAsset(name, data)
	if !ok || size != len(data) || size > maxMarkdownAssetBytes || asset.SHA256 != hash || kind != asset.ContentType {
		writeJSONError(w, 500, "page attachment integrity check failed")
		return
	}
	disposition := "attachment"
	if strings.HasPrefix(kind, "image/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
