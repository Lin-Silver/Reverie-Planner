package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

type DBProperty struct {
	ID         string `json:"id"`
	DatabaseID string `json:"databaseId"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Config     string `json:"config"`
	Position   int    `json:"position"`
	Visible    bool   `json:"visible"`
	Version    int    `json:"version"`
}

type DBRecord struct {
	ID         string            `json:"id"`
	DatabaseID string            `json:"databaseId"`
	Title      string            `json:"title"`
	PageID     string            `json:"pageId,omitempty"`
	Values     map[string]any    `json:"values,omitempty"`
	Version    int               `json:"version"`
	CreatedAt  string            `json:"createdAt"`
	UpdatedAt  string            `json:"updatedAt"`
}

type DBView struct {
	ID         string `json:"id"`
	DatabaseID string `json:"databaseId"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Config     string `json:"config"`
	Position   int    `json:"position"`
	Version    int    `json:"version"`
}

type Database struct {
	ID        string       `json:"id"`
	PageID    string       `json:"pageId"`
	Title     string       `json:"title"`
	Icon      string       `json:"icon"`
	Properties []DBProperty `json:"properties"`
	Views     []DBView     `json:"views"`
	Version   int          `json:"version"`
	CreatedAt string       `json:"createdAt"`
	UpdatedAt string       `json:"updatedAt"`
}

func (a *AppContext) handleDatabases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" {
			db, err := a.getDatabase(id)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					writeJSON(w, 404, map[string]string{"error": "not found"})
				} else {
					writeJSON(w, 500, map[string]string{"error": "could not read database"})
				}
				return
			}
			writeJSON(w, 200, db)
			return
		}
		rows, err := a.DB.Query(`SELECT id, page_id, title, icon, version, created_at, updated_at FROM databases WHERE deleted_at IS NULL ORDER BY created_at ASC`)
		if err != nil { writeJSON(w, 500, map[string]string{"error": err.Error()}); return }
		type rawDB struct {
			d       Database
			icon    sql.NullString
			created string
			updated string
		}
		var raws []rawDB
		for rows.Next() {
			var r rawDB
			if err := rows.Scan(&r.d.ID, &r.d.PageID, &r.d.Title, &r.icon, &r.d.Version, &r.created, &r.updated); err != nil { rows.Close(); writeJSON(w, 500, map[string]string{"error": err.Error()}); return }
			raws = append(raws, r)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		rows.Close()
		out := []Database{}
		for _, r := range raws {
			d := r.d
			d.Icon = r.icon.String
			d.CreatedAt = r.created
			d.UpdatedAt = r.updated
			d.Properties, err = a.listProperties(d.ID)
			if err != nil { writeJSON(w, 500, map[string]string{"error": err.Error()}); return }
			d.Views, err = a.listViews(d.ID)
			if err != nil { writeJSON(w, 500, map[string]string{"error": err.Error()}); return }
			out = append(out, d)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var d Database
		if _, err := decodeJSONRequest(r, &d); err != nil { writeJSON(w, 400, map[string]string{"error":"invalid request JSON"}); return }
		if d.Title == "" {
			d.Title = "Untitled database"
		}
		if d.ID == "" {
			d.ID = newID("db")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		tx, err := a.DB.Begin()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// An existing page may host a new database, but it must not already
		// back another database, record or linked view. Include soft-deleted owners so a
		// future restore cannot silently create two owners of the same page.
		if d.PageID != "" {
			var available int
			err := tx.QueryRow(`SELECT COUNT(*) FROM pages p WHERE p.id=?
				AND COALESCE(p.linked_database_id,'')=''
				AND COALESCE(p.linked_view_id,'')=''
				AND NOT EXISTS (SELECT 1 FROM databases d WHERE d.page_id=p.id)
				AND NOT EXISTS (SELECT 1 FROM db_records r WHERE r.page_id=p.id)`, d.PageID).Scan(&available)
			if err != nil {
				tx.Rollback()
				writeJSON(w, 500, map[string]string{"error": "could not validate database backing page"})
				return
			}
			if available != 1 {
				tx.Rollback()
				writeJSON(w, 409, map[string]string{"error": "database backing page is missing or already in use"})
				return
			}
		}
		if d.PageID == "" {
			pageID := newID("page")
			if _, err := tx.Exec(`INSERT INTO pages (id, title, icon, content, workspace, created_by, version) VALUES (?,?,?,?,1,'You',1)`, pageID, d.Title, nullStr(d.Icon), ""); err != nil {
				tx.Rollback()
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			d.PageID = pageID
		}
		if _, err := tx.Exec(`INSERT INTO databases (id, page_id, title, icon, version, created_at, updated_at) VALUES (?,?,?,?,1,?,?)`, d.ID, d.PageID, d.Title, nullStr(d.Icon), now, now); err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		pid := newID("prop")
		if _, err := tx.Exec(`INSERT INTO db_properties (id, database_id, name, type, config, position, visible, version) VALUES (?,?,?,?, '{}', 0, 1, 1)`, pid, d.ID, "Name", "title"); err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		vid := newID("view")
		if _, err := tx.Exec(`INSERT INTO db_views (id, database_id, name, type, config, position, version) VALUES (?,?,?,?, '{}', 0, 1)`, vid, d.ID, "Table", "table"); err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// Build the complete response before committing. A failure to read the
		// newly inserted schema must roll back the database and backing page;
		// never return a partial 201 for a committed, unreadable database.
		created, err := readDatabaseInTx(tx, d.ID)
		if err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": "could not read created database"})
			return
		}
		if err := tx.Commit(); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, created)
	case http.MethodPut:
		var d Database
		if _, err := decodeJSONRequest(r, &d); err != nil {
            writeJSON(w,400,map[string]string{"error":"invalid request JSON"});return
        }
		if d.ID=="" || d.Version<=0 || d.Title=="" {
            writeJSON(w,400,map[string]string{"error":"id, version and title are required"});return
        }
		now:=time.Now().UTC().Format(time.RFC3339)
        tx,err:=a.DB.Begin()
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        defer tx.Rollback()
        result,err:=tx.Exec(`UPDATE databases SET title=?,icon=?,version=version+1,updated_at=?
            WHERE id=? AND version=? AND deleted_at IS NULL`,d.Title,nullStr(d.Icon),now,d.ID,d.Version)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        changed,err:=result.RowsAffected()
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        if changed!=1 {writeJSON(w,409,map[string]string{"error":"version conflict"});return}
        // The database title and its backing page title are one logical edit.
        pageResult,err:=tx.Exec(`UPDATE pages SET title=?,updated_at=?,version=version+1
            WHERE id=(SELECT page_id FROM databases WHERE id=?)`,d.Title,now,d.ID)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        pageChanged,err:=pageResult.RowsAffected()
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        if pageChanged!=1 {writeJSON(w,409,map[string]string{"error":"database backing page missing"});return}
        // Materialize the full response before committing. A failed database,
        // property or view readback must roll back both this rename and the
        // backing-page version change rather than return 500 after a commit.
        out,err:=readDatabaseInTx(tx,d.ID)
        if err!=nil {writeJSON(w,500,map[string]string{"error":"could not read updated database"});return}
        if err:=tx.Commit();err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        writeJSON(w,200,out)
	case http.MethodDelete:
        id:=r.URL.Query().Get("id")
        result,err:=a.DB.Exec("UPDATE databases SET deleted_at=?,version=version+1 WHERE id=? AND deleted_at IS NULL",time.Now().UTC().Format(time.RFC3339),id)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        changed,err:=result.RowsAffected()
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        if changed!=1 {writeJSON(w,404,map[string]string{"error":"database not found"});return}
        w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) getDatabase(id string) (Database, error) {
	var d Database
	var icon sql.NullString
	var created, updated string
	err := a.DB.QueryRow(`SELECT id, page_id, title, icon, version, created_at, updated_at FROM databases WHERE id=? AND deleted_at IS NULL`, id).Scan(&d.ID, &d.PageID, &d.Title, &icon, &d.Version, &created, &updated)
	if err != nil {
		return d, err
	}
	d.Icon = icon.String
	d.CreatedAt = created
	d.UpdatedAt = updated
	d.Properties, err = a.listProperties(d.ID)
	if err != nil { return d, err }
	d.Views, err = a.listViews(d.ID)
	if err != nil { return d, err }
	return d, nil
}

// readDatabaseInTx constructs the entire response on the same SQLite
// transaction as a database mutation. A readback failure can then roll back
// the mutation, including the associated backing-page title/version update.
func readDatabaseInTx(tx *sql.Tx, id string) (Database, error) {
	var d Database
	var icon sql.NullString
	err := tx.QueryRow(`SELECT id, page_id, title, icon, version, created_at, updated_at
		FROM databases WHERE id=? AND deleted_at IS NULL`, id).
		Scan(&d.ID, &d.PageID, &d.Title, &icon, &d.Version, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	d.Icon = icon.String

	props, err := tx.Query(`SELECT id, database_id, name, type, config, position, visible, version
		FROM db_properties WHERE database_id=? AND deleted_at IS NULL ORDER BY position ASC`, id)
	if err != nil {
		return d, err
	}
	d.Properties = []DBProperty{}
	for props.Next() {
		var p DBProperty
		var visible int
		if err := props.Scan(&p.ID, &p.DatabaseID, &p.Name, &p.Type, &p.Config, &p.Position, &visible, &p.Version); err != nil {
			props.Close()
			return d, err
		}
		p.Visible = visible == 1
		d.Properties = append(d.Properties, p)
	}
	if err := props.Err(); err != nil {
		props.Close()
		return d, err
	}
	if err := props.Close(); err != nil {
		return d, err
	}

	views, err := tx.Query(`SELECT id, database_id, name, type, config, position, version
		FROM db_views WHERE database_id=? AND deleted_at IS NULL ORDER BY position ASC`, id)
	if err != nil {
		return d, err
	}
	d.Views = []DBView{}
	for views.Next() {
		var v DBView
		if err := views.Scan(&v.ID, &v.DatabaseID, &v.Name, &v.Type, &v.Config, &v.Position, &v.Version); err != nil {
			views.Close()
			return d, err
		}
		d.Views = append(d.Views, v)
	}
	if err := views.Err(); err != nil {
		views.Close()
		return d, err
	}
	if err := views.Close(); err != nil {
		return d, err
	}
	return d, nil
}

func (a *AppContext) listProperties(dbID string) ([]DBProperty, error) {
	rows, err := a.DB.Query(`SELECT id, database_id, name, type, config, position, visible, version FROM db_properties WHERE database_id=? AND deleted_at IS NULL ORDER BY position ASC`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBProperty{}
	for rows.Next() {
		var p DBProperty
		var visible int
		if err := rows.Scan(&p.ID, &p.DatabaseID, &p.Name, &p.Type, &p.Config, &p.Position, &visible, &p.Version); err != nil { return nil, err }
		p.Visible = visible == 1
		out = append(out, p)
	}
	if err := rows.Err(); err != nil { return nil, err }
	return out, nil
}

func (a *AppContext) listViews(dbID string) ([]DBView, error) {
	rows, err := a.DB.Query(`SELECT id, database_id, name, type, config, position, version FROM db_views WHERE database_id=? AND deleted_at IS NULL ORDER BY position ASC`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBView{}
	for rows.Next() {
		var v DBView
		if err := rows.Scan(&v.ID, &v.DatabaseID, &v.Name, &v.Type, &v.Config, &v.Position, &v.Version); err != nil { return nil, err }
		out = append(out, v)
	}
	if err := rows.Err(); err != nil { return nil, err }
	return out, nil
}

func (a *AppContext) handleDBProperties(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var p DBProperty
		if _, err := decodeJSONRequest(r, &p); err != nil { writeJSON(w, 400, map[string]string{"error":"invalid request JSON"}); return }
		p.Name = strings.TrimSpace(p.Name)
		if p.DatabaseID == "" || p.Name == "" {
            writeJSON(w,400,map[string]string{"error":"databaseId and name required"});return
        }
        var active int
        if err:=a.DB.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL",p.DatabaseID).Scan(&active);err!=nil {
            writeJSON(w,500,map[string]string{"error":err.Error()});return
        }
        if active!=1 {writeJSON(w,404,map[string]string{"error":"database not found"});return}
		if p.Type == "" {
			p.Type = "text"
		}
		// A database always starts with its sole required title property.
		// Additional title fields would violate its schema invariant.
		if p.Type == "title" {
			writeJSON(w, 400, map[string]string{"error": "database already has a title property"})
			return
		}
		if p.ID == "" {
			p.ID = newID("prop")
		}
		if p.Config == "" {
			p.Config = "{}"
		}
		if err := validatePropertyDefinition(p.Type, p.Config); err != nil { writeJSON(w, 400, map[string]string{"error": err.Error()}); return }
		if err:=validateDBComputedDefinition(a.DB,p.DatabaseID,p.Type,p.Config);err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
		// Owner/target validation, position reservation and insertion must be
		// atomic. Otherwise concurrent creators can reuse MAX(position), or
		// a property can be inserted after its owner is soft-deleted.
		tx, err := a.DB.Begin()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer tx.Rollback()
		if err := tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL", p.DatabaseID).Scan(&active); err != nil {
			writeJSON(w, 500, map[string]string{"error": "could not inspect property database"})
			return
		}
		if active != 1 {
			writeJSON(w, 404, map[string]string{"error": "database not found"})
			return
		}
		if p.Type == "relation" {
			var cfg struct {TargetDatabaseID string `json:"targetDatabaseId"`}
			_ = json.Unmarshal([]byte(p.Config), &cfg)
			var target int
			if err := tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL", cfg.TargetDatabaseID).Scan(&target); err != nil {
				writeJSON(w, 500, map[string]string{"error": "could not inspect relation target"})
				return
			}
			if target != 1 {
				writeJSON(w, 400, map[string]string{"error": "relation target database not found"})
				return
			}
		}
		// Reserve deleted property positions too: restore must not collide.
		var nextPosition int64
		if err := tx.QueryRow(`SELECT COALESCE(MAX(position),-1)+1 FROM db_properties WHERE database_id=?`, p.DatabaseID).Scan(&nextPosition); err != nil {
			writeJSON(w, 500, map[string]string{"error": "could not calculate property position"})
			return
		}
		if nextPosition < 0 || int64(int(nextPosition)) != nextPosition {
			writeJSON(w, 500, map[string]string{"error": "invalid property position"})
			return
		}
		p.Position = int(nextPosition)
		p.Visible = true
		p.Version = 1
		if _, err := tx.Exec(`INSERT INTO db_properties (id, database_id, name, type, config, position, visible, version) VALUES (?,?,?,?,?,?,1,1)`, p.ID, p.DatabaseID, p.Name, p.Type, p.Config, p.Position); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// Read the inserted row before commit. If SQLite accepted the INSERT
		// but the schema cannot be read back, the creation must roll back.
		var created DBProperty
		var createdVisible int
		if err := tx.QueryRow(`SELECT id, database_id, name, type, config, position, visible, version
			FROM db_properties WHERE id=? AND database_id=? AND deleted_at IS NULL`, p.ID, p.DatabaseID).
			Scan(&created.ID, &created.DatabaseID, &created.Name, &created.Type, &created.Config,
				&created.Position, &createdVisible, &created.Version); err != nil {
			writeJSON(w, 500, map[string]string{"error": "could not read created property"})
			return
		}
		created.Visible = createdVisible == 1
		if err := tx.Commit(); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, created)
	case http.MethodPut:
		var p DBProperty
		if _, err := decodeJSONRequest(r, &p); err != nil { writeJSON(w, 400, map[string]string{"error":"invalid request JSON"}); return }
		if p.ID == "" || p.Version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "id and version required"})
			return
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "property name is required"})
			return
		}
		var currentType, actualDatabaseID, currentConfig string
		if err := a.DB.QueryRow(`SELECT p.type, p.database_id, p.config FROM db_properties p
			JOIN databases d ON d.id=p.database_id AND d.deleted_at IS NULL
			WHERE p.id=? AND p.deleted_at IS NULL`, p.ID).Scan(&currentType, &actualDatabaseID, &currentConfig); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, 404, map[string]string{"error": "property not found"})
			} else {
				writeJSON(w, 500, map[string]string{"error": "could not inspect property"})
			}
			return
		}
		// The supplied databaseId must not redirect computed-field validation
		// to a different database from the one owning this property.
		if p.DatabaseID != actualDatabaseID {
			writeJSON(w, 400, map[string]string{"error": "property databaseId does not match its owner"})
			return
		}
		if currentType != p.Type { writeJSON(w, 400, map[string]string{"error": "changing a property type requires a data migration"}); return }
		if currentType == "title" && !p.Visible { writeJSON(w, 400, map[string]string{"error": "title property must remain visible"}); return }
		if p.Config == "" { p.Config = "{}" }
		if currentType != "title" { if err := validatePropertyDefinition(p.Type, p.Config); err != nil { writeJSON(w, 400, map[string]string{"error": err.Error()}); return } }
		if err:=validateDBComputedDefinition(a.DB,actualDatabaseID,p.Type,p.Config);err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
		if currentType == "relation" {
			var cfg struct { TargetDatabaseID string `json:"targetDatabaseId"` }
			if err:=json.Unmarshal([]byte(p.Config), &cfg);err!=nil || cfg.TargetDatabaseID=="" {
				writeJSON(w,400,map[string]string{"error":"invalid relation target"})
				return
			}
			var activeTarget int
			if err:=a.DB.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL",cfg.TargetDatabaseID).Scan(&activeTarget);err!=nil {
				writeJSON(w,500,map[string]string{"error":"could not verify relation target"})
				return
			}
			if activeTarget!=1 {writeJSON(w,400,map[string]string{"error":"relation target database not found"});return}
		}
		visible := 0
		if p.Visible {
			visible = 1
		}
		// Validate existing values and update the schema in one transaction. This
		// includes values on soft-deleted records, which may later be restored.
		tx, err := a.DB.Begin()
		if err != nil { writeJSON(w, 500, map[string]string{"error": "could not begin property update"}); return }
		defer tx.Rollback()
		if (currentType == "select" || currentType == "multi_select") && p.Config != currentConfig {
			compatible, err := validateSelectOptionChange(tx, p.ID, currentType, p.Config)
			if err != nil { writeJSON(w, 500, map[string]string{"error": "could not inspect existing property values"}); return }
			if !compatible { writeJSON(w, 409, map[string]string{"error": "property options conflict with existing record values"}); return }
		}
		if currentType == "relation" && p.Config != currentConfig {
			compatible, err := validateRelationTargetChange(tx, p.ID, currentConfig, p.Config)
			if err != nil { writeJSON(w, 500, map[string]string{"error": "could not inspect existing relation values"}); return }
			if !compatible { writeJSON(w, 409, map[string]string{"error": "relation target conflicts with existing record values"}); return }
		}
		res, err := tx.Exec(`UPDATE db_properties SET name=?, type=?, config=?, visible=?, version=version+1
			WHERE id=? AND database_id=? AND version=? AND deleted_at IS NULL AND database_id IN (SELECT id FROM databases WHERE deleted_at IS NULL)`,
			p.Name, p.Type, p.Config, visible, p.ID, actualDatabaseID, p.Version)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		n, err := res.RowsAffected()
		if err != nil {
			writeJSON(w,500,map[string]string{"error":"could not verify property update"})
			return
		}
		if n != 1 {
			writeJSON(w, 409, map[string]string{"error": "version conflict"})
			return
		}
		// fetch
		var out DBProperty
		var vis int
		if err := tx.QueryRow(`SELECT id, database_id, name, type, config, position, visible, version FROM db_properties WHERE id=?`, p.ID).Scan(&out.ID, &out.DatabaseID, &out.Name, &out.Type, &out.Config, &out.Position, &vis, &out.Version); err != nil {
			writeJSON(w,500,map[string]string{"error":"could not read updated property"})
			return
		}
		out.Visible = vis == 1
		if err := tx.Commit(); err != nil { writeJSON(w, 500, map[string]string{"error": "could not commit property update"}); return }
		writeJSON(w, 200, out)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" { writeJSON(w, 400, map[string]string{"error": "property id required"}); return }
		var kind string
		if err := a.DB.QueryRow(`SELECT type FROM db_properties WHERE id=? AND deleted_at IS NULL`, id).Scan(&kind); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, 404, map[string]string{"error": "property not found"})
			} else {
				writeJSON(w, 500, map[string]string{"error": "could not inspect property"})
			}
			return
		}
		if kind == "title" { writeJSON(w, 400, map[string]string{"error": "title property cannot be deleted"}); return }
        result,err:=a.DB.Exec("UPDATE db_properties SET deleted_at=?,version=version+1 WHERE id=? AND deleted_at IS NULL",time.Now().UTC().Format(time.RFC3339),id)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        changed,err:=result.RowsAffected()
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        if changed!=1 {writeJSON(w,404,map[string]string{"error":"property not found"});return}
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleDBRecords(w http.ResponseWriter, r *http.Request) {
	dbID := r.URL.Query().Get("databaseId")
	switch r.Method {
	case http.MethodGet:
		if dbID == "" {
			writeJSON(w, 400, map[string]string{"error": "databaseId required"})
			return
		}
		var active int
        if err:=a.DB.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL",dbID).Scan(&active);err!=nil {
            writeJSON(w,500,map[string]string{"error":err.Error()});return
        }
        if active!=1 {writeJSON(w,404,map[string]string{"error":"database not found"});return}
        rows,err := a.DB.Query(`SELECT id, database_id, title, page_id, version, created_at, updated_at FROM db_records WHERE database_id=? AND deleted_at IS NULL ORDER BY created_at ASC`, dbID)
		if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
		type rawRec struct {
			rec    DBRecord
			pageID sql.NullString
			created, updated string
		}
		var raws []rawRec
		for rows.Next() {
			var r rawRec
			if err:=rows.Scan(&r.rec.ID, &r.rec.DatabaseID, &r.rec.Title, &r.pageID, &r.rec.Version, &r.created, &r.updated);err!=nil {rows.Close();writeJSON(w,500,map[string]string{"error":err.Error()});return}
			raws = append(raws, r)
		}
		if err:=rows.Err();err!=nil {rows.Close();writeJSON(w,500,map[string]string{"error":err.Error()});return}
		rows.Close()
		fields,err:=a.listProperties(dbID)
		if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
		valueIndex,err:=a.recordValuesForDatabase(dbID)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
		out := []DBRecord{}
		for _, r := range raws {
			rec := r.rec
			rec.PageID = r.pageID.String
			rec.CreatedAt = r.created
			rec.UpdatedAt = r.updated
			rec.Values=valueIndex[rec.ID]
            if rec.Values==nil {rec.Values=map[string]any{}}
			appendDBSystemValues(rec.Values, fields, rec)
			if err:=a.appendComputedValues(fields,&rec);err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
			out = append(out, rec)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var rec DBRecord
		if _, err := decodeJSONRequest(r, &rec); err != nil { writeJSON(w, 400, map[string]string{"error":"invalid request JSON"}); return }
		if rec.DatabaseID == "" {
			writeJSON(w, 400, map[string]string{"error": "databaseId required"})
			return
		}
		// Initial property values are not yet supported by the create path.
		// Refuse them instead of silently returning a record without the values
		// supplied by the caller. The versioned PUT endpoint supports these edits.
		if len(rec.Values) != 0 {
			writeJSON(w, 400, map[string]string{"error": "initial property values must be saved through record update"})
			return
		}
		if rec.PageID != "" {
			writeJSON(w, 400, map[string]string{"error": "new records must create their own backing page"})
			return
		}
		if rec.ID == "" {
			rec.ID = newID("rec")
		}
		if rec.Title == "" {
			rec.Title = "Untitled"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		tx, err := a.DB.Begin()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
        var active int
        if err:=tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL",rec.DatabaseID).Scan(&active);err!=nil {
            tx.Rollback();writeJSON(w,500,map[string]string{"error":err.Error()});return
        }
        if active!=1 {tx.Rollback();writeJSON(w,404,map[string]string{"error":"database not found"});return}
        _, err = tx.Exec(`INSERT INTO db_records (id, database_id, title, page_id, version, created_at, updated_at) VALUES (?,?,?,?,1,?,?)`, rec.ID, rec.DatabaseID, rec.Title, nullStr(rec.PageID), now, now)
		if err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if rec.PageID == "" {
			pageID := newID("page")
			if _, err := tx.Exec(`INSERT INTO pages (id, title, icon, content, workspace, created_by, version) VALUES (?,?,?,?,1,'You',1)`, pageID, rec.Title, "", ""); err != nil {
				tx.Rollback()
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			if _, err := tx.Exec(`UPDATE db_records SET page_id=? WHERE id=?`, pageID, rec.ID); err != nil {
				tx.Rollback()
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			rec.PageID = pageID
		}
		// Materialize the record and its backing page before commit. A trigger
		// or damaged schema can make an inserted row unreadable; in that case
		// neither the record nor its newly created page may be published.
		var created DBRecord
		var createdPageID sql.NullString
		var pageTitle string
		var pageVersion int
		if err := tx.QueryRow(`SELECT r.id, r.database_id, r.title, r.page_id,
			r.version, r.created_at, r.updated_at, p.title, p.version
			FROM db_records r JOIN pages p ON p.id=r.page_id
			WHERE r.id=? AND r.database_id=? AND r.deleted_at IS NULL`,
			rec.ID, rec.DatabaseID).Scan(
			&created.ID, &created.DatabaseID, &created.Title, &createdPageID,
			&created.Version, &created.CreatedAt, &created.UpdatedAt,
			&pageTitle, &pageVersion); err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": "could not read created record and backing page"})
			return
		}
		if !createdPageID.Valid || createdPageID.String == "" ||
			pageTitle != created.Title || pageVersion <= 0 || created.Version <= 0 {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": "created record and backing page are inconsistent"})
			return
		}
		created.PageID = createdPageID.String
		created.Values = map[string]any{}
		if err := tx.Commit(); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, created)
	case http.MethodPut:
		var body struct {
			ID       string         `json:"id"`
			Title    string         `json:"title"`
			Values   map[string]any `json:"values"`
			Version  int            `json:"version"`
		}
		if _, err := decodeJSONRequest(r, &body); err != nil { writeJSON(w, 400, map[string]string{"error":"invalid request JSON"}); return }
		if body.ID == "" || body.Version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "id and version required"})
			return
		}
		// get db id for version check
		var dbID string
		if err := a.DB.QueryRow(`SELECT r.database_id FROM db_records r JOIN databases d ON d.id=r.database_id AND d.deleted_at IS NULL WHERE r.id=? AND r.deleted_at IS NULL`, body.ID).Scan(&dbID); err != nil {
			if errors.Is(err, sql.ErrNoRows) { writeJSON(w, 404, map[string]string{"error": "record not found"}) } else { writeJSON(w, 500, map[string]string{"error": err.Error()}) }
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		tx, err := a.DB.Begin()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// Keep the backing page version stable during property-only edits.
        var previousTitle string
        if err:=tx.QueryRow("SELECT title FROM db_records WHERE id=?",body.ID).Scan(&previousTitle);err!=nil {
			tx.Rollback()
			if errors.Is(err, sql.ErrNoRows) { writeJSON(w,404,map[string]string{"error":"record not found"}) } else { writeJSON(w,500,map[string]string{"error":err.Error()}) }
			return
		}
		// version check on record
		res, err := tx.Exec(`UPDATE db_records SET title=COALESCE(NULLIF(?,''),title), version=version+1, updated_at=? WHERE id=? AND version=? AND deleted_at IS NULL`, body.Title, now, body.ID, body.Version)
		if err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		n, err := res.RowsAffected()
		if err != nil { tx.Rollback(); writeJSON(w,500,map[string]string{"error":err.Error()}); return }
		if n == 0 {
			tx.Rollback()
			writeJSON(w, 409, map[string]string{"error": "version conflict"})
			return
		}
		// validate property ownership and values
		type propDefinition struct { kind, config string }
        definitions := map[string]propDefinition{}
        propRows, err := tx.Query(`SELECT id, type, config FROM db_properties WHERE database_id=? AND deleted_at IS NULL`, dbID)
        if err != nil { tx.Rollback(); writeJSON(w, 500, map[string]string{"error": err.Error()}); return }
        for propRows.Next() {
            var pid, ptype, cfg string
            if err := propRows.Scan(&pid, &ptype, &cfg); err != nil { propRows.Close(); tx.Rollback(); writeJSON(w, 500, map[string]string{"error": err.Error()}); return }
            definitions[pid] = propDefinition{kind: ptype, config: cfg}
        }
        if err := propRows.Err(); err != nil { propRows.Close(); tx.Rollback(); writeJSON(w, 500, map[string]string{"error": err.Error()}); return }
        propRows.Close()
        for propID, val := range body.Values {
            def, ok := definitions[propID]
			if !ok {
				tx.Rollback()
				writeJSON(w, 400, map[string]string{"error": "unknown property"})
				return
			}
			if def.kind=="title" {tx.Rollback();writeJSON(w,400,map[string]string{"error":"title must be changed through record title"});return}
			if err := validatePropertyValue(def.kind, val); err != nil {
				tx.Rollback()
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			if err := validateConfiguredPropertyValue(def.kind, def.config, val); err != nil { tx.Rollback(); writeJSON(w, 400, map[string]string{"error": err.Error()}); return }
			if err := validateDBReferenceValue(tx,def.kind,def.config,val); err != nil { tx.Rollback(); writeJSON(w, 400, map[string]string{"error": err.Error()}); return }
            if def.kind=="file" {if err:=validateDBFileReferences(tx,body.ID,val);err!=nil{tx.Rollback();writeJSON(w,400,map[string]string{"error":err.Error()});return}}
			vb, err := json.Marshal(val)
			if err != nil { tx.Rollback(); writeJSON(w, 400, map[string]string{"error": "invalid property value"}); return }
			// upsert
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM db_values WHERE record_id=? AND property_id=?`, body.ID, propID).Scan(&exists); err != nil {
				tx.Rollback()
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			if exists > 0 {
				if _, err := tx.Exec(`UPDATE db_values SET value=?, version=version+1, updated_at=? WHERE record_id=? AND property_id=?`, string(vb), now, body.ID, propID); err != nil {
					tx.Rollback()
					writeJSON(w, 500, map[string]string{"error": err.Error()})
					return
				}
			} else {
				if _, err := tx.Exec(`INSERT INTO db_values (record_id, property_id, value, version, updated_at) VALUES (?,?,?,1,?)`, body.ID, propID, string(vb), now); err != nil {
					tx.Rollback()
					writeJSON(w, 500, map[string]string{"error": err.Error()})
					return
				}
			}
		}
		if body.Title!="" && body.Title!=previousTitle {
            pageRes,err:=tx.Exec("UPDATE pages SET title=?,updated_at=?,version=version+1 WHERE id=(SELECT page_id FROM db_records WHERE id=?)",body.Title,now,body.ID)
            if err!=nil {tx.Rollback();writeJSON(w,500,map[string]string{"error":err.Error()});return}
            count,err:=pageRes.RowsAffected()
            if err!=nil {tx.Rollback();writeJSON(w,500,map[string]string{"error":err.Error()});return}
            if count!=1 {tx.Rollback();writeJSON(w,409,map[string]string{"error":"linked record page missing"});return}
        } else {
            var exists int
            if err:=tx.QueryRow("SELECT COUNT(*) FROM pages WHERE id=(SELECT page_id FROM db_records WHERE id=?)",body.ID).Scan(&exists);err!=nil {tx.Rollback();writeJSON(w,500,map[string]string{"error":err.Error()});return}
            if exists!=1 {tx.Rollback();writeJSON(w,409,map[string]string{"error":"linked record page missing"});return}
        }
		// Build the complete response (including computed values) before commit.
		// An unreadable record, page, property or rollup must not publish the edit.
		rec, err := readDBRecordInTx(tx, body.ID, body.Title != "" && body.Title != previousTitle)
		if err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": "could not read updated record"})
			return
		}
		if err := tx.Commit(); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, rec)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" { writeJSON(w,400,map[string]string{"error":"id required"}); return }
		tx, err := a.DB.Begin()
		if err != nil { writeJSON(w,500,map[string]string{"error":err.Error()}); return }
		defer tx.Rollback()
		var targetDatabaseID string
		if err := tx.QueryRow(`SELECT r.database_id FROM db_records r
			JOIN databases d ON d.id=r.database_id AND d.deleted_at IS NULL
			WHERE r.id=? AND r.deleted_at IS NULL`, id).Scan(&targetDatabaseID); err != nil {
			if errors.Is(err,sql.ErrNoRows) { writeJSON(w,404,map[string]string{"error":"record not found"}) } else { writeJSON(w,500,map[string]string{"error":err.Error()}) }
			return
		}
		// A live source record must never retain a dangling relation to this
		// record. Restrict deletion rather than silently rewriting user values.
		blocked, err := hasIncomingActiveRelation(tx, id, targetDatabaseID)
		if err != nil { writeJSON(w,500,map[string]string{"error":"could not inspect incoming relations"}); return }
		if blocked { writeJSON(w,409,map[string]string{"error":"record is referenced by another record; remove relations first"}); return }
		result, err := tx.Exec("UPDATE db_records SET deleted_at=?,version=version+1 WHERE id=? AND deleted_at IS NULL",time.Now().UTC().Format(time.RFC3339),id)
		if err != nil { writeJSON(w,500,map[string]string{"error":err.Error()}); return }
		changed, err := result.RowsAffected()
		if err != nil { writeJSON(w,500,map[string]string{"error":err.Error()}); return }
		if changed!=1 { writeJSON(w,409,map[string]string{"error":"record changed during deletion"}); return }
		if err := tx.Commit(); err != nil { writeJSON(w,500,map[string]string{"error":err.Error()}); return }
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) getDBRecord(id string) (DBRecord, error) {
	var rec DBRecord
	var pageID sql.NullString
	var created, updated string
	err := a.DB.QueryRow(`SELECT r.id, r.database_id, r.title, r.page_id, r.version, r.created_at, r.updated_at FROM db_records r JOIN databases d ON d.id=r.database_id AND d.deleted_at IS NULL WHERE r.id=? AND r.deleted_at IS NULL`, id).Scan(&rec.ID, &rec.DatabaseID, &rec.Title, &pageID, &rec.Version, &created, &updated)
	if err != nil {
		return rec, err
	}
	rec.PageID = pageID.String
	rec.CreatedAt = created
	rec.UpdatedAt = updated
	rec.Values,err = a.recordValues(rec.ID)
    if err!=nil {return rec,err}
	fields,err:=a.listProperties(rec.DatabaseID)
	if err!=nil {return rec,err}
	appendDBSystemValues(rec.Values,fields,rec)
	if err:=a.appendComputedValues(fields,&rec);err!=nil {return rec,err}
	return rec, nil
}

func (a *AppContext) recordValues(recordID string) (map[string]any,error) {
    out:=map[string]any{}
    rows,err:=a.DB.Query("SELECT v.property_id,v.value FROM db_values v JOIN db_properties p ON p.id=v.property_id AND p.deleted_at IS NULL WHERE v.record_id=?",recordID)
    if err!=nil{return nil,err}
    defer rows.Close()
    for rows.Next(){
        var propertyID,raw string
        if err:=rows.Scan(&propertyID,&raw);err!=nil{return nil,err}
        var value any
        if err:=json.Unmarshal([]byte(raw),&value);err!=nil{return nil,fmt.Errorf("invalid stored JSON for property %s: %w",propertyID,err)}
        out[propertyID]=value
    }
    if err:=rows.Err();err!=nil{return nil,err}
    return out,nil
}


// Read values in one indexed join rather than one query per record. The
// database has a single SQLite connection; all rows close before related
// rollup computations execute.
func (a *AppContext) recordValuesForDatabase(databaseID string) (map[string]map[string]any,error) {
    rows,err:=a.DB.Query(`SELECT dv.record_id,dv.property_id,dv.value
        FROM db_records dr
        JOIN db_values dv ON dv.record_id=dr.id
        JOIN db_properties dp ON dp.id=dv.property_id AND dp.deleted_at IS NULL
        WHERE dr.database_id=? AND dr.deleted_at IS NULL`,databaseID)
    if err!=nil{return nil,err}
    defer rows.Close()
    output:=map[string]map[string]any{}
    for rows.Next() {
        var recordID,propertyID,raw string
        if err:=rows.Scan(&recordID,&propertyID,&raw);err!=nil{return nil,err}
        var value any
        if err:=json.Unmarshal([]byte(raw),&value);err!=nil{return nil,fmt.Errorf("corrupt property %s of record %s: %w",propertyID,recordID,err)}
        if output[recordID]==nil{output[recordID]=map[string]any{}}
        output[recordID][propertyID]=value
    }
    if err:=rows.Err();err!=nil{return nil,err}
    return output,nil
}

func (a *AppContext) handleDBViews(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var v DBView
		if _, err := decodeJSONRequest(r, &v); err != nil { writeJSON(w, 400, map[string]string{"error":"invalid request JSON"}); return }
		if v.DatabaseID == "" {
            writeJSON(w,400,map[string]string{"error":"databaseId required"});return
        }
		if v.Name == "" {
			v.Name = "View"
		}
		if v.Type == "" {
			v.Type = "table"
		}
		if v.ID == "" {
			v.ID = newID("view")
		}
		if v.Config == "" {
			v.Config = "{}"
		}
		if err := validateDBViewConfig(v.Type, v.Config); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// Reserve the next position and verify the owner in the same
		// transaction as insertion. Concurrent view creators must not both
		// observe the same MAX(position), or create a view after its database
		// has been soft-deleted.
		tx, err := a.DB.Begin()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer tx.Rollback()
		var active int
		if err := tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL", v.DatabaseID).Scan(&active); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if active != 1 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "database not found"})
			return
		}
		var maxPos sql.NullInt64
		if err := tx.QueryRow(`SELECT COALESCE(MAX(position),-1)+1 FROM db_views WHERE database_id=?`, v.DatabaseID).Scan(&maxPos); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not determine view position"})
			return
		}
		v.Position = int(maxPos.Int64)
		v.Version = 1
		if _, err := tx.Exec(`INSERT INTO db_views (id, database_id, name, type, config, position, version) VALUES (?,?,?,?,?,?,1)`, v.ID, v.DatabaseID, v.Name, v.Type, v.Config, v.Position); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Materialize the response inside the INSERT transaction so an
		// unreadable view cannot be committed behind a failed HTTP response.
		var created DBView
		if err := tx.QueryRow(`SELECT id, database_id, name, type, config, position, version
			FROM db_views WHERE id=? AND database_id=? AND deleted_at IS NULL`, v.ID, v.DatabaseID).
			Scan(&created.ID, &created.DatabaseID, &created.Name, &created.Type, &created.Config,
				&created.Position, &created.Version); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read created view"})
			return
		}
		if err := tx.Commit(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, created)
	case http.MethodPut:
		var v DBView
		if _, err := decodeJSONRequest(r, &v); err != nil { writeJSON(w, 400, map[string]string{"error":"invalid request JSON"}); return }
		if v.ID == "" || v.Version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "id and version required"})
			return
		}
		if v.Config == "" {v.Config="{}"}
        if err:=validateDBViewConfig(v.Type,v.Config);err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
		// Keep the versioned update and response readback in one transaction.
		// A readback failure must not leave a committed change that the client
		// only knows as an HTTP 500 (and may then retry with a stale version).
		tx, err := a.DB.Begin()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "could not begin view update"})
			return
		}
		defer tx.Rollback()
		res, err := tx.Exec(`UPDATE db_views SET name=?, type=?, config=?, version=version+1 WHERE id=? AND version=? AND deleted_at IS NULL AND database_id IN (SELECT id FROM databases WHERE deleted_at IS NULL)`, v.Name, v.Type, v.Config, v.ID, v.Version)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		n, err := res.RowsAffected()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if n != 1 {
			writeJSON(w, 409, map[string]string{"error": "version conflict"})
			return
		}
		if err := tx.QueryRow(`SELECT id, database_id, name, type, config, position, version FROM db_views WHERE id=?`, v.ID).Scan(&v.ID, &v.DatabaseID, &v.Name, &v.Type, &v.Config, &v.Position, &v.Version); err != nil {
			writeJSON(w, 500, map[string]string{"error": "could not read updated view"})
			return
		}
		if err := tx.Commit(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "could not commit view update"})
			return
		}
		writeJSON(w, 200, v)
	case http.MethodDelete:
        id:=r.URL.Query().Get("id")
        if id == "" { writeJSON(w, http.StatusBadRequest, map[string]string{"error": "view id required"}); return }
        tx,err:=a.DB.Begin()
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        defer tx.Rollback()
        var dbID string
        if err:=tx.QueryRow("SELECT database_id FROM db_views WHERE id=? AND deleted_at IS NULL",id).Scan(&dbID);err!=nil {
            if err==sql.ErrNoRows {writeJSON(w,404,map[string]string{"error":"view not found"})} else {writeJSON(w,500,map[string]string{"error":"could not inspect view"})}
            return
        }
        var refs int
        if err:=tx.QueryRow("SELECT COUNT(*) FROM pages WHERE linked_view_id=?",id).Scan(&refs);err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        if refs!=0 {writeJSON(w,409,map[string]string{"error":"linked view is still used by pages"});return}
        var total int
        if err:=tx.QueryRow("SELECT COUNT(*) FROM db_views WHERE database_id=? AND deleted_at IS NULL",dbID).Scan(&total);err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        if total<=1 {writeJSON(w,409,map[string]string{"error":"cannot remove last database view"});return}
        deleted,err:=tx.Exec("UPDATE db_views SET deleted_at=?,version=version+1 WHERE id=? AND deleted_at IS NULL",time.Now().UTC().Format(time.RFC3339),id)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        count,err:=deleted.RowsAffected()
        if err!=nil || count!=1 {writeJSON(w,409,map[string]string{"error":"view changed during deletion"});return}
        if err:=tx.Commit();err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        w.WriteHeader(204)
	case http.MethodGet:
		dbID := r.URL.Query().Get("databaseId")
		if dbID == "" {
			writeJSON(w, 400, map[string]string{"error": "databaseId required"})
			return
		}
		// An empty view list is not evidence that the requested database exists.
		// Reject missing and soft-deleted owners, and do not mask SQLite failures.
		var active int
		if err := a.DB.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL", dbID).Scan(&active); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not inspect database"})
			return
		}
		if active != 1 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "database not found"})
			return
		}
		vs,err:=a.listViews(dbID)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
		writeJSON(w,200,vs)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleDBLinkedView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		DatabaseID string `json:"databaseId"`
		PageID     string `json:"pageId"`
		Name       string `json:"name"`
	}
	if _, err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request JSON"})
		return
	}
	if body.DatabaseID == "" || body.PageID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "databaseId and pageId required"})
		return
	}

	// The new view and its page binding are a single logical mutation.
	// Check ownership inside the same transaction, including soft-deleted
	// owners, so a linked page cannot overwrite an existing link or become
	// a second owner of a database/record backing page.
	tx, err := a.DB.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not begin linked view creation"})
		return
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL", body.DatabaseID).Scan(&active); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not inspect source database"})
		return
	}
	if active != 1 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "database not found"})
		return
	}
	var linkedDatabaseID, linkedViewID sql.NullString
	if err := tx.QueryRow("SELECT linked_database_id, linked_view_id FROM pages WHERE id=?", body.PageID).Scan(&linkedDatabaseID, &linkedViewID); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "page not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not inspect linked view page"})
		}
		return
	}
	if linkedDatabaseID.String != "" || linkedViewID.String != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "page already has a linked view"})
		return
	}
	var backingOwners int
	if err := tx.QueryRow(`SELECT
		(SELECT COUNT(*) FROM databases WHERE page_id=?) +
		(SELECT COUNT(*) FROM db_records WHERE page_id=?)`, body.PageID, body.PageID).Scan(&backingOwners); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not inspect page ownership"})
		return
	}
	if backingOwners != 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "page is already a database or record backing page"})
		return
	}
	v := DBView{ID: newID("view"), DatabaseID: body.DatabaseID, Name: body.Name, Type: "table", Config: "{}"}
	if v.Name == "" {
		v.Name = "Linked view"
	}
	if err := tx.QueryRow("SELECT COALESCE(MAX(position),-1)+1 FROM db_views WHERE database_id=?", body.DatabaseID).Scan(&v.Position); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not determine linked view position"})
		return
	}
	v.Version = 1
	if _, err := tx.Exec(`INSERT INTO db_views (id, database_id, name, type, config, position, version) VALUES (?,?,?,?,?,?,1)`,
		v.ID, v.DatabaseID, v.Name, v.Type, v.Config, v.Position); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create linked view"})
		return
	}
	// The guarded update also protects against an intervening binding.
	// Bump the page version so an older BlockNote draft cannot silently
	// acknowledge a page snapshot predating the new linked view.
	pageRes, err := tx.Exec(`UPDATE pages SET linked_database_id=?, linked_view_id=?, updated_at=?, version=version+1
		WHERE id=? AND COALESCE(linked_database_id,'')='' AND COALESCE(linked_view_id,'')=''
		AND NOT EXISTS (SELECT 1 FROM databases WHERE page_id=pages.id)
		AND NOT EXISTS (SELECT 1 FROM db_records WHERE page_id=pages.id)`,
		body.DatabaseID, v.ID, time.Now().UTC().Format(time.RFC3339), body.PageID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not bind linked view page"})
		return
	}
	changed, err := pageRes.RowsAffected()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not verify linked view page binding"})
		return
	}
	if changed != 1 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "page is no longer available for a linked view"})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not commit linked view"})
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func validatePropertyValue(propType string, val any) error {
    if propType=="created_time" || propType=="last_edited_time" || propType=="formula" || propType=="rollup" { return fmt.Errorf("computed property is read-only") }
	if val == nil {
		return nil
	}
	switch propType {
	case "number":
		switch val.(type) {
		case float64, int, int64, json.Number:
		default:
			return fmt.Errorf("invalid number value")
		}
	case "checkbox":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("invalid checkbox value")
		}
	case "select":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("invalid select value")
		}
	case "multi_select":
		arr, ok := val.([]any)
		if !ok {
			return fmt.Errorf("invalid multi_select value")
		}
		for _, x := range arr {
			if _, ok := x.(string); !ok {
				return fmt.Errorf("invalid multi_select item")
			}
		}
	case "date":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("invalid date value")
		}
		if s != "" {
			if _, err := time.Parse(time.RFC3339, s); err != nil {
				if _, err2 := time.Parse("2006-01-02", s); err2 != nil {
					return fmt.Errorf("invalid date format")
				}
			}
		}
	case "url", "email", "text", "title", "person":
        if _, ok := val.(string); !ok { return fmt.Errorf("invalid %s value", propType) }
    case "file":
        values,ok:=val.([]any);if !ok{return fmt.Errorf("file references must be an array")}
        for _,v:=range values{if _,ok:=v.(string);!ok{return fmt.Errorf("invalid file ID")}}
    case "relation":
        values,ok:=val.([]any); if !ok {return fmt.Errorf("relation must be an array of IDs")}
        for _,v:=range values {if _,ok:=v.(string);!ok{return fmt.Errorf("invalid relation ID")}}
    case "created_time","last_edited_time":
        return fmt.Errorf("system timestamp is read-only")
    default:
        return fmt.Errorf("unsupported property type: %s", propType)
    }
    return nil
}

func validateConfiguredPropertyValue(kind, config string, val any) error {
    if val == nil { return nil }
    if kind == "email" && val != "" {
        input := val.(string)
        addr, err := mail.ParseAddress(input)
        if err != nil || addr.Address != input { return fmt.Errorf("invalid email address") }
    }
    if kind == "url" && val != "" {
        u, err := url.Parse(val.(string))
        if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" { return fmt.Errorf("invalid URL") }
    }
    if kind == "select" || kind == "multi_select" {
        var def struct { Options []string `json:"options"` }
        if err := json.Unmarshal([]byte(config), &def); err != nil { return fmt.Errorf("invalid select property config") }
        if len(def.Options) == 0 { return nil }
        allowed := map[string]bool{}
        for _, option := range def.Options { allowed[option] = true }
        check := func(v string) error { if v == "" || allowed[v] { return nil }; return fmt.Errorf("option %q is not configured", v) }
        if kind == "select" { return check(val.(string)) }
        for _, x := range val.([]any) { if err := check(x.(string)); err != nil { return err } }
    }
    return nil
}

func validatePropertyDefinition(kind, config string) error {
    switch kind {
    case "text", "number", "checkbox", "date", "email", "url", "person", "file", "created_time", "last_edited_time":
        return nil
    case "formula":
        _,err:=parseFormulaDefinition(config);return err
    case "rollup":
        _,err:=parseRollupDefinition(config);return err
    case "relation":
        var v struct { TargetDatabaseID string `json:"targetDatabaseId"` }
        if err:=json.Unmarshal([]byte(config),&v);err!=nil || v.TargetDatabaseID=="" {return fmt.Errorf("relation targetDatabaseId required")}
        return nil
    case "select", "multi_select":
        if config == "" { return nil }
        var cfg map[string]json.RawMessage
        if err := json.Unmarshal([]byte(config), &cfg); err != nil { return fmt.Errorf("invalid options JSON") }
        if raw, ok := cfg["options"]; ok {
            var options []string
            if err := json.Unmarshal(raw, &options); err != nil { return fmt.Errorf("options must be a string array") }
            seen := map[string]bool{}
            for _, option := range options {
                if option == "" || seen[option] { return fmt.Errorf("options must be unique nonempty strings") }
                seen[option] = true
            }
        }
        return nil
    default:
        return fmt.Errorf("unsupported property type: %s", kind)
    }
}


func validateDBViewConfig(kind,config string) error {
    switch kind {
    case "table","board","calendar","list","gallery":
    default:return fmt.Errorf("unsupported database view type: %s",kind)
    }
    if len(config)>64*1024{return fmt.Errorf("view config exceeds 64KB")}
    var body map[string]json.RawMessage
    if err:=json.Unmarshal([]byte(config),&body);err!=nil{return fmt.Errorf("view config must be a JSON object: %w",err)}
    if body==nil{return fmt.Errorf("view config must be an object")}
    if raw,ok:=body["sortRules"];ok{
        var rules []struct {
            PropertyID string `json:"propertyId"`
            Direction string `json:"direction"`
        }
        if err:=json.Unmarshal(raw,&rules);err!=nil || rules==nil{return fmt.Errorf("sortRules must be a non-null array")}
        if len(rules)>5{return fmt.Errorf("sortRules exceeds five fields")}
        seen:=map[string]bool{}
        for _,rule:=range rules{
            if rule.PropertyID=="" || len(rule.PropertyID)>128 || seen[rule.PropertyID] {
                return fmt.Errorf("sortRules propertyIds must be nonempty and unique")
            }
            if rule.Direction!="asc" && rule.Direction!="desc" {return fmt.Errorf("sortRules direction must be asc or desc")}
            seen[rule.PropertyID]=true
        }
    }
    return nil
}
