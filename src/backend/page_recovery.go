package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// SQL triggers preserve pre-change content in the same transaction, including
// writes made by database records and page moves rather than the editor API.
var pageRecoverySQL = []string{
	`CREATE TABLE page_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, page_id TEXT NOT NULL, version INTEGER NOT NULL, title TEXT NOT NULL, snapshot TEXT NOT NULL, saved_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
	`CREATE INDEX idx_page_revisions_page ON page_revisions(page_id,id)`,
	`CREATE TABLE page_trash (id TEXT PRIMARY KEY, version INTEGER NOT NULL, title TEXT NOT NULL, snapshot TEXT NOT NULL, deleted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
}

const oldPageSnapshot = `json_object('id',OLD.id,'projectId',OLD.project_id,'workspace',json(CASE WHEN OLD.workspace=1 THEN 'true' ELSE 'false' END),'parentId',OLD.parent_id,'title',OLD.title,'icon',OLD.icon,'cover',OLD.cover,'content',coalesce(OLD.content,''),'createdBy',OLD.created_by,'createdAt',strftime('%Y-%m-%dT%H:%M:%SZ',OLD.created_at),'updatedAt',strftime('%Y-%m-%dT%H:%M:%SZ',OLD.updated_at),'version',OLD.version,'linkedDatabaseId',OLD.linked_database_id,'linkedViewId',OLD.linked_view_id)`

var pageRecoveryTriggers = map[string]string{
	"page_revision_capture": `CREATE TRIGGER page_revision_capture BEFORE UPDATE ON pages WHEN OLD.title IS NOT NEW.title OR OLD.content IS NOT NEW.content OR OLD.icon IS NOT NEW.icon OR OLD.cover IS NOT NEW.cover BEGIN INSERT INTO page_revisions(page_id,version,title,snapshot) VALUES(OLD.id,OLD.version,OLD.title,` + oldPageSnapshot + `); END`,
	"page_trash_capture":    `CREATE TRIGGER page_trash_capture BEFORE DELETE ON pages BEGIN INSERT INTO page_trash(id,version,title,snapshot) VALUES(OLD.id,OLD.version,OLD.title,` + oldPageSnapshot + `); END`,
	"page_trash_reserve_id": `CREATE TRIGGER page_trash_reserve_id BEFORE INSERT ON pages WHEN EXISTS(SELECT 1 FROM page_trash WHERE id=NEW.id) BEGIN SELECT RAISE(ABORT,'restore the trashed page before reusing its ID'); END`,
}

func migratePageRecovery(db *sql.DB) error {
	var applied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=11").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return verifyPageRecoverySchema(db)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range pageRecoverySQL {
		if _, err = tx.Exec(statement); err != nil {
			return err
		}
	}
	for _, statement := range pageRecoveryTriggers {
		if _, err = tx.Exec(statement); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES(11, CURRENT_TIMESTAMP)"); err != nil {
		return err
	}
	return tx.Commit()
}

func verifyPageRecoverySchema(db *sql.DB) error {
	for table, columns := range map[string][]string{
		"page_revisions": {"id", "page_id", "version", "title", "snapshot", "saved_at"},
		"page_trash":     {"id", "version", "title", "snapshot", "deleted_at"},
	} {
		if err := verifyMigrationTableColumns(db, table, columns); err != nil {
			return err
		}
	}
	for name, want := range pageRecoveryTriggers {
		var statement string
		if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?", name).Scan(&statement); err != nil {
			return fmt.Errorf("page recovery trigger %s missing: %w", name, err)
		}
		if statement != want {
			return fmt.Errorf("page recovery trigger %s changed; back up and inspect before repair", name)
		}
	}
	return verifyAppliedMigrationIndexes(db, 11)
}

type pageRecoveryItem struct {
	ID       int64           `json:"id"`
	PageID   string          `json:"pageId"`
	Version  int             `json:"version"`
	Title    string          `json:"title"`
	SavedAt  string          `json:"savedAt"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

func (a *AppContext) handlePageHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("pageId")
		if id == "" {
			writeJSONError(w, 400, "pageId required")
			return
		}
		if revision := r.URL.Query().Get("revisionId"); revision != "" {
			var item pageRecoveryItem
			var snapshot string
			err := a.DB.QueryRow("SELECT id,page_id,version,title,saved_at,snapshot FROM page_revisions WHERE page_id=? AND id=?", id, revision).Scan(&item.ID, &item.PageID, &item.Version, &item.Title, &item.SavedAt, &snapshot)
			if err != nil {
				recoveryReadError(w, err)
				return
			}
			item.Snapshot = json.RawMessage(snapshot)
			writeJSON(w, 200, item)
			return
		}
		before := int64(0)
		if raw := r.URL.Query().Get("before"); raw != "" {
			var err error
			before, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || before <= 0 {
				writeJSONError(w, 400, "invalid cursor")
				return
			}
		}
		rows, err := a.DB.Query("SELECT id,page_id,version,title,saved_at FROM page_revisions WHERE page_id=? AND (?=0 OR id<?) ORDER BY id DESC LIMIT 100", id, before, before)
		if err != nil {
			recoveryReadError(w, err)
			return
		}
		defer rows.Close()
		items := []pageRecoveryItem{}
		for rows.Next() {
			var item pageRecoveryItem
			if err := rows.Scan(&item.ID, &item.PageID, &item.Version, &item.Title, &item.SavedAt); err != nil {
				recoveryReadError(w, err)
				return
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			recoveryReadError(w, err)
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var request struct {
		PageID     string `json:"pageId"`
		RevisionID int64  `json:"revisionId"`
		Version    int    `json:"version"`
	}
	if _, err := decodeJSONRequest(r, &request); err != nil || request.PageID == "" || request.RevisionID <= 0 || request.Version <= 0 {
		writeJSONError(w, 400, "pageId, revisionId and current version required")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		recoveryReadError(w, err)
		return
	}
	defer tx.Rollback()
	var snapshot string
	if err = tx.QueryRow("SELECT snapshot FROM page_revisions WHERE page_id=? AND id=?", request.PageID, request.RevisionID).Scan(&snapshot); err != nil {
		recoveryReadError(w, err)
		return
	}
	var old Page
	if err = json.Unmarshal([]byte(snapshot), &old); err != nil {
		recoveryReadError(w, err)
		return
	}
	// Structural metadata stays current. Database/record titles are managed by
	// their owning APIs; keep current titles rather than desynchronizing them.
	var owners int
	if err = tx.QueryRow("SELECT (SELECT COUNT(*) FROM databases WHERE page_id=?)+(SELECT COUNT(*) FROM db_records WHERE page_id=?)", request.PageID, request.PageID).Scan(&owners); err != nil {
		recoveryReadError(w, err)
		return
	}
	res, err := tx.Exec("UPDATE pages SET title=CASE WHEN ? THEN title ELSE ? END,content=?,icon=?,cover=?,version=version+1,updated_at=? WHERE id=? AND version=?", owners > 0, old.Title, old.Content, nullStr(old.Icon), nullStr(old.Cover), timeNowUTC(), request.PageID, request.Version)
	if err != nil {
		recoveryReadError(w, err)
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		recoveryReadError(w, err)
		return
	}
	if n != 1 {
		writeJSONError(w, 409, "version conflict; close history and reload the page before restoring")
		return
	}
	if err = tx.Commit(); err != nil {
		recoveryReadError(w, err)
		return
	}
	writeJSON(w, 200, map[string]int{"version": request.Version + 1})
}

func recoveryReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, 404, "page or revision not found")
	} else {
		writeJSONError(w, 500, "page recovery could not be completed")
	}
}

func (a *AppContext) deletePageToTrash(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSONError(w, 400, "id required")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		recoveryReadError(w, err)
		return
	}
	defer tx.Rollback()
	var current int
	if err = tx.QueryRow("SELECT version FROM pages WHERE id=?", id).Scan(&current); err != nil {
		recoveryReadError(w, err)
		return
	}
	version, err := strconv.Atoi(r.URL.Query().Get("version"))
	if err != nil || version <= 0 {
		writeJSONError(w, 400, "current version required")
		return
	}
	if version != current {
		writeJSONError(w, 409, "version conflict; reload before moving to trash")
		return
	}
	var children, owners int
	if err = tx.QueryRow("SELECT COUNT(*) FROM pages WHERE parent_id=?", id).Scan(&children); err != nil {
		recoveryReadError(w, err)
		return
	}
	if err = tx.QueryRow("SELECT (SELECT COUNT(*) FROM databases WHERE page_id=?)+(SELECT COUNT(*) FROM db_records WHERE page_id=?)", id, id).Scan(&owners); err != nil {
		recoveryReadError(w, err)
		return
	}
	if children > 0 || owners > 0 {
		writeJSONError(w, 409, "move child pages first; use database trash for database-owned pages")
		return
	}
	result, err := tx.Exec("DELETE FROM pages WHERE id=? AND version=?", id, version)
	if err != nil {
		recoveryReadError(w, err)
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		recoveryReadError(w, err)
		return
	}
	if changed != 1 {
		writeJSONError(w, 409, "page deletion was not applied")
		return
	}
	if err = tx.Commit(); err != nil {
		recoveryReadError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *AppContext) handlePageTrash(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := a.DB.Query("SELECT id,version,title,deleted_at FROM page_trash ORDER BY deleted_at DESC,id LIMIT 250")
		if err != nil {
			recoveryReadError(w, err)
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, title, at string
			var version int
			if err = rows.Scan(&id, &version, &title, &at); err != nil {
				recoveryReadError(w, err)
				return
			}
			items = append(items, map[string]any{"id": id, "title": title, "version": version, "deletedAt": at})
		}
		if err = rows.Err(); err != nil {
			recoveryReadError(w, err)
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var request struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	if _, err := decodeJSONRequest(r, &request); err != nil || request.ID == "" || request.Version <= 0 {
		writeJSONError(w, 400, "id and trash version required")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		recoveryReadError(w, err)
		return
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow("SELECT snapshot FROM page_trash WHERE id=? AND version=?", request.ID, request.Version).Scan(&raw); err != nil {
		recoveryReadError(w, err)
		return
	}
	var p Page
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		recoveryReadError(w, err)
		return
	}
	if p.ParentID != "" {
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM pages WHERE id=?", p.ParentID).Scan(&count); err != nil {
			recoveryReadError(w, err)
			return
		}
		if count != 1 {
			writeJSONError(w, 409, "restore parent page first")
			return
		}
	}
	if _, err = tx.Exec("DELETE FROM page_trash WHERE id=? AND version=?", request.ID, request.Version); err != nil {
		recoveryReadError(w, err)
		return
	}
	_, err = tx.Exec(`INSERT INTO pages(id,project_id,workspace,parent_id,title,icon,cover,content,created_by,created_at,updated_at,version,linked_database_id,linked_view_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, nullStr(p.ProjectID), p.Workspace, nullStr(p.ParentID), p.Title, nullStr(p.Icon), nullStr(p.Cover), p.Content, p.CreatedBy, p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), timeNowUTC(), p.Version+1, nullStr(p.LinkedDatabaseID), nullStr(p.LinkedViewID))
	if err != nil {
		recoveryReadError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		recoveryReadError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": p.ID, "version": p.Version + 1})
}
