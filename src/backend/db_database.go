package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
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
				writeJSON(w, 404, map[string]string{"error": "not found"})
				return
			}
			writeJSON(w, 200, db)
			return
		}
		rows, _ := a.DB.Query(`SELECT id, page_id, title, icon, version, created_at, updated_at FROM databases ORDER BY created_at ASC`)
		type rawDB struct {
			d       Database
			icon    sql.NullString
			created string
			updated string
		}
		var raws []rawDB
		for rows.Next() {
			var r rawDB
			_ = rows.Scan(&r.d.ID, &r.d.PageID, &r.d.Title, &r.icon, &r.d.Version, &r.created, &r.updated)
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
			d.Properties, _ = a.listProperties(d.ID)
			d.Views, _ = a.listViews(d.ID)
			out = append(out, d)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var d Database
		_ = json.NewDecoder(r.Body).Decode(&d)
		if d.Title == "" {
			d.Title = "Untitled database"
		}
		if d.ID == "" {
			d.ID = newID("db")
		}
		if d.PageID == "" {
			// create a backing page
			pageID := newID("page")
			_, _ = a.DB.Exec(`INSERT INTO pages (id, title, icon, content, workspace, created_by, version) VALUES (?,?,?,?,1,'You',1)`, pageID, d.Title, nullStr(d.Icon), "")
			d.PageID = pageID
		}
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := a.DB.Exec(`INSERT INTO databases (id, page_id, title, icon, version, created_at, updated_at) VALUES (?,?,?,?,1,?,?)`, d.ID, d.PageID, d.Title, nullStr(d.Icon), now, now)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// default title property
		pid := newID("prop")
		_, _ = a.DB.Exec(`INSERT INTO db_properties (id, database_id, name, type, config, position, visible, version) VALUES (?,?,?,?, '{}', 0, 1, 1)`, pid, d.ID, "Name", "title")
		// default view
		vid := newID("view")
		_, _ = a.DB.Exec(`INSERT INTO db_views (id, database_id, name, type, config, position, version) VALUES (?,?,?,?, '{}', 0, 1)`, vid, d.ID, "Table", "table")
		d.Version = 1
		d.CreatedAt = now
		d.UpdatedAt = now
		d.Properties, _ = a.listProperties(d.ID)
		d.Views, _ = a.listViews(d.ID)
		writeJSON(w, 201, d)
	case http.MethodPut:
		var d Database
		_ = json.NewDecoder(r.Body).Decode(&d)
		if d.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id required"})
			return
		}
		if d.Version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "version required"})
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		res, err := a.DB.Exec(`UPDATE databases SET title=?, icon=?, version=version+1, updated_at=? WHERE id=? AND version=?`, d.Title, nullStr(d.Icon), now, d.ID, d.Version)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeJSON(w, 409, map[string]string{"error": "version conflict"})
			return
		}
		// sync page title
		_, _ = a.DB.Exec(`UPDATE pages SET title=?, updated_at=? WHERE id=(SELECT page_id FROM databases WHERE id=?)`, d.Title, now, d.ID)
		out, _ := a.getDatabase(d.ID)
		writeJSON(w, 200, out)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM databases WHERE id=?`, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) getDatabase(id string) (Database, error) {
	var d Database
	var icon sql.NullString
	var created, updated string
	err := a.DB.QueryRow(`SELECT id, page_id, title, icon, version, created_at, updated_at FROM databases WHERE id=?`, id).Scan(&d.ID, &d.PageID, &d.Title, &icon, &d.Version, &created, &updated)
	if err != nil {
		return d, err
	}
	d.Icon = icon.String
	d.CreatedAt = created
	d.UpdatedAt = updated
	d.Properties, _ = a.listProperties(d.ID)
	d.Views, _ = a.listViews(d.ID)
	return d, nil
}

func (a *AppContext) listProperties(dbID string) ([]DBProperty, error) {
	rows, err := a.DB.Query(`SELECT id, database_id, name, type, config, position, visible, version FROM db_properties WHERE database_id=? ORDER BY position ASC`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBProperty{}
	for rows.Next() {
		var p DBProperty
		var visible int
		_ = rows.Scan(&p.ID, &p.DatabaseID, &p.Name, &p.Type, &p.Config, &p.Position, &visible, &p.Version)
		p.Visible = visible == 1
		out = append(out, p)
	}
	return out, nil
}

func (a *AppContext) listViews(dbID string) ([]DBView, error) {
	rows, err := a.DB.Query(`SELECT id, database_id, name, type, config, position, version FROM db_views WHERE database_id=? ORDER BY position ASC`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBView{}
	for rows.Next() {
		var v DBView
		_ = rows.Scan(&v.ID, &v.DatabaseID, &v.Name, &v.Type, &v.Config, &v.Position, &v.Version)
		out = append(out, v)
	}
	return out, nil
}

func (a *AppContext) handleDBProperties(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var p DBProperty
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p.DatabaseID == "" || p.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "databaseId and name required"})
			return
		}
		if p.Type == "" {
			p.Type = "text"
		}
		if p.ID == "" {
			p.ID = newID("prop")
		}
		if p.Config == "" {
			p.Config = "{}"
		}
		// position: append
		var maxPos sql.NullInt64
		_ = a.DB.QueryRow(`SELECT COALESCE(MAX(position),-1)+1 FROM db_properties WHERE database_id=?`, p.DatabaseID).Scan(&maxPos)
		p.Position = int(maxPos.Int64)
		p.Visible = true
		p.Version = 1
		_, err := a.DB.Exec(`INSERT INTO db_properties (id, database_id, name, type, config, position, visible, version) VALUES (?,?,?,?,?,?,1,1)`, p.ID, p.DatabaseID, p.Name, p.Type, p.Config, p.Position)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, p)
	case http.MethodPut:
		var p DBProperty
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p.ID == "" || p.Version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "id and version required"})
			return
		}
		visible := 0
		if p.Visible {
			visible = 1
		}
		res, err := a.DB.Exec(`UPDATE db_properties SET name=?, type=?, config=?, visible=?, version=version+1 WHERE id=? AND version=?`, p.Name, p.Type, p.Config, visible, p.ID, p.Version)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeJSON(w, 409, map[string]string{"error": "version conflict"})
			return
		}
		// fetch
		var out DBProperty
		var vis int
		_ = a.DB.QueryRow(`SELECT id, database_id, name, type, config, position, visible, version FROM db_properties WHERE id=?`, p.ID).Scan(&out.ID, &out.DatabaseID, &out.Name, &out.Type, &out.Config, &out.Position, &vis, &out.Version)
		out.Visible = vis == 1
		writeJSON(w, 200, out)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM db_properties WHERE id=?`, id)
		// delete values
		_, _ = a.DB.Exec(`DELETE FROM db_values WHERE property_id=?`, id)
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
		rows, _ := a.DB.Query(`SELECT id, database_id, title, page_id, version, created_at, updated_at FROM db_records WHERE database_id=? ORDER BY created_at ASC`, dbID)
		type rawRec struct {
			rec    DBRecord
			pageID sql.NullString
			created, updated string
		}
		var raws []rawRec
		for rows.Next() {
			var r rawRec
			_ = rows.Scan(&r.rec.ID, &r.rec.DatabaseID, &r.rec.Title, &r.pageID, &r.rec.Version, &r.created, &r.updated)
			raws = append(raws, r)
		}
		rows.Close()
		out := []DBRecord{}
		for _, r := range raws {
			rec := r.rec
			rec.PageID = r.pageID.String
			rec.CreatedAt = r.created
			rec.UpdatedAt = r.updated
			rec.Values = a.recordValues(rec.ID)
			out = append(out, rec)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var rec DBRecord
		_ = json.NewDecoder(r.Body).Decode(&rec)
		if rec.DatabaseID == "" {
			writeJSON(w, 400, map[string]string{"error": "databaseId required"})
			return
		}
		if rec.ID == "" {
			rec.ID = newID("rec")
		}
		if rec.Title == "" {
			rec.Title = "Untitled"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := a.DB.Exec(`INSERT INTO db_records (id, database_id, title, page_id, version, created_at, updated_at) VALUES (?,?,?,?,1,?,?)`, rec.ID, rec.DatabaseID, rec.Title, nullStr(rec.PageID), now, now)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// create backing page if none
		if rec.PageID == "" {
			pageID := newID("page")
			_, _ = a.DB.Exec(`INSERT INTO pages (id, title, icon, content, workspace, created_by, version) VALUES (?,?,?,?,1,'You',1)`, pageID, rec.Title, "", "")
			_, _ = a.DB.Exec(`UPDATE db_records SET page_id=? WHERE id=?`, pageID, rec.ID)
			rec.PageID = pageID
		}
		rec.Version = 1
		rec.CreatedAt = now
		rec.UpdatedAt = now
		rec.Values = map[string]any{}
		writeJSON(w, 201, rec)
	case http.MethodPut:
		var body struct {
			ID       string         `json:"id"`
			Title    string         `json:"title"`
			Values   map[string]any `json:"values"`
			Version  int            `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ID == "" || body.Version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "id and version required"})
			return
		}
		// get db id for version check
		var dbID string
		_ = a.DB.QueryRow(`SELECT database_id FROM db_records WHERE id=?`, body.ID).Scan(&dbID)
		now := time.Now().UTC().Format(time.RFC3339)
		tx, err := a.DB.Begin()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// version check on record
		res, err := tx.Exec(`UPDATE db_records SET title=COALESCE(NULLIF(?,''),title), version=version+1, updated_at=? WHERE id=? AND version=?`, body.Title, now, body.ID, body.Version)
		if err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			tx.Rollback()
			writeJSON(w, 409, map[string]string{"error": "version conflict"})
			return
		}
		// validate property ownership and values
		propType := map[string]string{}
		propRows, _ := tx.Query(`SELECT id, type FROM db_properties WHERE database_id=?`, dbID)
		for propRows.Next() {
			var pid, ptype string
			_ = propRows.Scan(&pid, &ptype)
			propType[pid] = ptype
		}
		propRows.Close()
		for propID, val := range body.Values {
			pt, ok := propType[propID]
			if !ok {
				tx.Rollback()
				writeJSON(w, 400, map[string]string{"error": "unknown property"})
				return
			}
			if err := validatePropertyValue(pt, val); err != nil {
				tx.Rollback()
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			vb, _ := json.Marshal(val)
			// upsert
			var exists int
			_ = tx.QueryRow(`SELECT COUNT(*) FROM db_values WHERE record_id=? AND property_id=?`, body.ID, propID).Scan(&exists)
			if exists > 0 {
				_, _ = tx.Exec(`UPDATE db_values SET value=?, version=version+1, updated_at=? WHERE record_id=? AND property_id=?`, string(vb), now, body.ID, propID)
			} else {
				_, _ = tx.Exec(`INSERT INTO db_values (record_id, property_id, value, version, updated_at) VALUES (?,?,?,1,?)`, body.ID, propID, string(vb), now)
			}
		}
		if err := tx.Commit(); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// sync page title
		_, _ = a.DB.Exec(`UPDATE pages SET title=?, updated_at=? WHERE id=(SELECT page_id FROM db_records WHERE id=?)`, body.Title, now, body.ID)
		rec, _ := a.getDBRecord(body.ID)
		writeJSON(w, 200, rec)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM db_records WHERE id=?`, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) getDBRecord(id string) (DBRecord, error) {
	var rec DBRecord
	var pageID sql.NullString
	var created, updated string
	err := a.DB.QueryRow(`SELECT id, database_id, title, page_id, version, created_at, updated_at FROM db_records WHERE id=?`, id).Scan(&rec.ID, &rec.DatabaseID, &rec.Title, &pageID, &rec.Version, &created, &updated)
	if err != nil {
		return rec, err
	}
	rec.PageID = pageID.String
	rec.CreatedAt = created
	rec.UpdatedAt = updated
	rec.Values = a.recordValues(rec.ID)
	return rec, nil
}

func (a *AppContext) recordValues(recordID string) map[string]any {
	out := map[string]any{}
	rows, _ := a.DB.Query(`SELECT property_id, value FROM db_values WHERE record_id=?`, recordID)
	defer rows.Close()
	for rows.Next() {
		var pid, val string
		_ = rows.Scan(&pid, &val)
		var v any
		_ = json.Unmarshal([]byte(val), &v)
		out[pid] = v
	}
	return out
}

func (a *AppContext) handleDBViews(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var v DBView
		_ = json.NewDecoder(r.Body).Decode(&v)
		if v.DatabaseID == "" {
			writeJSON(w, 400, map[string]string{"error": "databaseId required"})
			return
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
		var maxPos sql.NullInt64
		_ = a.DB.QueryRow(`SELECT COALESCE(MAX(position),-1)+1 FROM db_views WHERE database_id=?`, v.DatabaseID).Scan(&maxPos)
		v.Position = int(maxPos.Int64)
		v.Version = 1
		_, err := a.DB.Exec(`INSERT INTO db_views (id, database_id, name, type, config, position, version) VALUES (?,?,?,?,?,?,1)`, v.ID, v.DatabaseID, v.Name, v.Type, v.Config, v.Position)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, v)
	case http.MethodPut:
		var v DBView
		_ = json.NewDecoder(r.Body).Decode(&v)
		if v.ID == "" || v.Version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "id and version required"})
			return
		}
		if v.Config == "" {
			v.Config = "{}"
		}
		res, err := a.DB.Exec(`UPDATE db_views SET name=?, type=?, config=?, version=version+1 WHERE id=? AND version=?`, v.Name, v.Type, v.Config, v.ID, v.Version)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeJSON(w, 409, map[string]string{"error": "version conflict"})
			return
		}
		_ = a.DB.QueryRow(`SELECT id, database_id, name, type, config, position, version FROM db_views WHERE id=?`, v.ID).Scan(&v.ID, &v.DatabaseID, &v.Name, &v.Type, &v.Config, &v.Position, &v.Version)
		writeJSON(w, 200, v)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM db_views WHERE id=?`, id)
		w.WriteHeader(204)
	case http.MethodGet:
		dbID := r.URL.Query().Get("databaseId")
		if dbID == "" {
			writeJSON(w, 400, map[string]string{"error": "databaseId required"})
			return
		}
		vs, _ := a.listViews(dbID)
		writeJSON(w, 200, vs)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleDBLinkedView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var body struct {
		DatabaseID string `json:"databaseId"`
		PageID     string `json:"pageId"`
		Name       string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DatabaseID == "" || body.PageID == "" {
		writeJSON(w, 400, map[string]string{"error": "databaseId and pageId required"})
		return
	}
	v := DBView{ID: newID("view"), DatabaseID: body.DatabaseID, Name: body.Name, Type: "table", Config: "{}"}
	if v.Name == "" {
		v.Name = "Linked view"
	}
	var maxPos sql.NullInt64
	_ = a.DB.QueryRow(`SELECT COALESCE(MAX(position),-1)+1 FROM db_views WHERE database_id=?`, body.DatabaseID).Scan(&maxPos)
	v.Position = int(maxPos.Int64)
	v.Version = 1
	_, err := a.DB.Exec(`INSERT INTO db_views (id, database_id, name, type, config, position, version) VALUES (?,?,?,?,?,?,1)`, v.ID, v.DatabaseID, v.Name, v.Type, v.Config, v.Position)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, _ = a.DB.Exec(`UPDATE pages SET linked_database_id=?, linked_view_id=?, updated_at=? WHERE id=?`, body.DatabaseID, v.ID, time.Now().UTC().Format(time.RFC3339), body.PageID)
	writeJSON(w, 201, v)
}

func validatePropertyValue(propType string, val any) error {
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
	case "url", "email", "text", "title":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("invalid %s value", propType)
		}
	}
	return nil
}
