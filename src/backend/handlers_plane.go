package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var idCounter uint64

func newID(prefix string) string {
	n := atomic.AddUint64(&idCounter, 1)
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatUint(n, 10)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *AppContext) handleLabels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := r.URL.Query().Get("projectId")
		q := `SELECT id, project_id, name, color, created_at FROM labels`
		args := []any{}
		if projectID != "" {
			q += ` WHERE project_id=?`
			args = append(args, projectID)
		}
		q += ` ORDER BY created_at DESC`
		rows, _ := a.DB.Query(q, args...)
		defer rows.Close()
		out := []Label{}
		for rows.Next() {
			var l Label
			var created string
			var pid sqlNullString
			_ = rows.Scan(&l.ID, &pid, &l.Name, &l.Color, &created)
			l.ProjectID = pid.String
			l.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, l)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var l Label
		if _, err := decodeJSONRequest(r, &l); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid label payload")
			return
		}
		if l.ID == "" {
			l.ID = newID("label")
		}
		if strings.TrimSpace(l.Name) == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		if l.Color == "" {
			l.Color = "gray"
		}
		l.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO labels (id, project_id, name, color, created_at) VALUES (?,?,?,?,?)`, l.ID, nullStr(l.ProjectID), l.Name, l.Color, l.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "label could not be saved")
			return
		}
		writeJSON(w, 201, l)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		result, err := a.DB.Exec(`DELETE FROM labels WHERE id=?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "label could not be deleted")
			return
		}
		rows, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "label deletion could not be verified")
			return
		}
		if rows == 0 {
			writeJSONError(w, http.StatusNotFound, "label not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleStates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := r.URL.Query().Get("projectId")
		q := `SELECT id, project_id, name, color, group_name, sequence, created_at FROM states`
		args := []any{}
		if projectID != "" {
			q += ` WHERE project_id=?`
			args = append(args, projectID)
		}
		q += ` ORDER BY sequence ASC`
		rows, _ := a.DB.Query(q, args...)
		defer rows.Close()
		out := []State{}
		for rows.Next() {
			var s State
			var created string
			_ = rows.Scan(&s.ID, &s.ProjectID, &s.Name, &s.Color, &s.Group, &s.Sequence, &created)
			s.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, s)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var s State
		if _, err := decodeJSONRequest(r, &s); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid state payload")
			return
		}
		if s.ID == "" {
			s.ID = newID("state")
		}
		if strings.TrimSpace(s.Name) == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		if strings.TrimSpace(s.ProjectID) == "" {
			writeJSONError(w, http.StatusBadRequest, "projectId required")
			return
		}
		if s.Color == "" {
			s.Color = "gray"
		}
		if s.Group == "" {
			s.Group = "backlog"
		}
		s.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO states (id, project_id, name, color, group_name, sequence, created_at) VALUES (?,?,?,?,?,?,?)`, s.ID, s.ProjectID, s.Name, s.Color, s.Group, s.Sequence, s.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "state could not be saved")
			return
		}
		writeJSON(w, 201, s)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		result, err := a.DB.Exec(`DELETE FROM states WHERE id=?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "state could not be deleted")
			return
		}
		rows, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "state deletion could not be verified")
			return
		}
		if rows == 0 {
			writeJSONError(w, http.StatusNotFound, "state not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleCycles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := r.URL.Query().Get("projectId")
		q := `SELECT id, project_id, name, description, start_date, end_date, status, created_at FROM cycles`
		args := []any{}
		if projectID != "" {
			q += ` WHERE project_id=?`
			args = append(args, projectID)
		}
		q += ` ORDER BY created_at DESC`
		rows, _ := a.DB.Query(q, args...)
		defer rows.Close()
		out := []Cycle{}
		for rows.Next() {
			var c Cycle
			var created string
			var desc, start, end sqlNullString
			_ = rows.Scan(&c.ID, &c.ProjectID, &c.Name, &desc, &start, &end, &c.Status, &created)
			c.Description = desc.String
			c.StartDate = start.String
			c.EndDate = end.String
			c.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, c)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var c Cycle
		if _, err := decodeJSONRequest(r, &c); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid cycle payload")
			return
		}
		if c.ID == "" {
			c.ID = newID("cycle")
		}
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.ProjectID) == "" {
			writeJSONError(w, http.StatusBadRequest, "name and projectId required")
			return
		}
		if c.Status == "" {
			c.Status = "draft"
		}
		if (c.StartDate != "" && !validISODate(c.StartDate)) || (c.EndDate != "" && !validISODate(c.EndDate)) {
			writeJSONError(w, http.StatusBadRequest, "invalid cycle date")
			return
		}
		c.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, err := a.DB.Exec(`INSERT INTO cycles (id, project_id, name, description, start_date, end_date, status, created_at) VALUES (?,?,?,?,?,?,?,?)`, c.ID, c.ProjectID, c.Name, nullStr(c.Description), nullStr(c.StartDate), nullStr(c.EndDate), c.Status, c.CreatedAt.Format(time.RFC3339))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "cycle could not be saved")
			return
		}
		writeJSON(w, 201, c)
	case http.MethodPut:
		var c Cycle
		fields, err := decodeJSONRequest(r, &c)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid cycle payload")
			return
		}
		if strings.TrimSpace(c.ID) == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		var existing Cycle
		var description, startDate, endDate sqlNullString
		err = a.DB.QueryRow(`SELECT project_id, name, description, start_date, end_date, status FROM cycles WHERE id=?`, c.ID).
			Scan(&existing.ProjectID, &existing.Name, &description, &startDate, &endDate, &existing.Status)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "cycle not found")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "cycle could not be loaded")
			return
		}
		existing.Description, existing.StartDate, existing.EndDate = description.String, startDate.String, endDate.String
		if _, ok := fields["projectId"]; ok && c.ProjectID != existing.ProjectID {
			writeJSONError(w, http.StatusBadRequest, "projectId cannot be changed")
			return
		}
		if _, ok := fields["name"]; !ok { c.Name = existing.Name }
		if _, ok := fields["description"]; !ok { c.Description = existing.Description }
		if _, ok := fields["startDate"]; !ok { c.StartDate = existing.StartDate }
		if _, ok := fields["endDate"]; !ok { c.EndDate = existing.EndDate }
		if _, ok := fields["status"]; !ok { c.Status = existing.Status }
		if strings.TrimSpace(c.Name) == "" {
			writeJSONError(w, http.StatusBadRequest, "name required")
			return
		}
		for _, key := range []string{"startDate", "endDate"} {
			if _, ok := fields[key]; ok {
				value := c.StartDate
				if key == "endDate" { value = c.EndDate }
				if value != "" && !validISODate(value) {
					writeJSONError(w, http.StatusBadRequest, "invalid cycle date")
					return
				}
			}
		}
		result, err := a.DB.Exec(`UPDATE cycles SET name=?, description=?, start_date=?, end_date=?, status=? WHERE id=?`,
			c.Name, nullStr(c.Description), nullStr(c.StartDate), nullStr(c.EndDate), c.Status, c.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "cycle could not be updated")
			return
		}
		changed, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "cycle update could not be verified")
			return
		}
		if changed == 0 {
			writeJSONError(w, http.StatusNotFound, "cycle not found")
			return
		}
		c.ProjectID = existing.ProjectID
		writeJSON(w, http.StatusOK, c)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if strings.TrimSpace(id) == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		result, err := a.DB.Exec(`DELETE FROM cycles WHERE id=?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "cycle could not be deleted")
			return
		}
		changed, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "cycle deletion could not be verified")
			return
		}
		if changed == 0 {
			writeJSONError(w, http.StatusNotFound, "cycle not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleModules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := r.URL.Query().Get("projectId")
		q := `SELECT id, project_id, name, description, status, lead, start_date, end_date, created_at FROM modules`
		args := []any{}
		if projectID != "" {
			q += ` WHERE project_id=?`
			args = append(args, projectID)
		}
		q += ` ORDER BY created_at DESC`
		rows, err := a.DB.Query(q, args...)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "modules could not be loaded")
			return
		}
		defer rows.Close()
		out := []Module{}
		for rows.Next() {
			var m Module
			var created string
			var desc, lead, start, end sqlNullString
			if err := rows.Scan(&m.ID, &m.ProjectID, &m.Name, &desc, &m.Status, &lead, &start, &end, &created); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "modules could not be read")
				return
			}
			m.Description, m.Lead, m.StartDate, m.EndDate = desc.String, lead.String, start.String, end.String
			m.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, m)
		}
		if err := rows.Err(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "modules could not be read")
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var m Module
		if _, err := decodeJSONRequest(r, &m); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid module payload")
			return
		}
		if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.ProjectID) == "" {
			writeJSONError(w, http.StatusBadRequest, "name and projectId required")
			return
		}
		if (m.StartDate != "" && !validISODate(m.StartDate)) || (m.EndDate != "" && !validISODate(m.EndDate)) {
			writeJSONError(w, http.StatusBadRequest, "invalid module date")
			return
		}
		if m.ID == "" {
			m.ID = newID("module")
		}
		if m.Status == "" {
			m.Status = "backlog"
		}
		m.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO modules (id, project_id, name, description, status, lead, start_date, end_date, created_at) VALUES (?,?,?,?,?,?,?,?,?)`, m.ID, m.ProjectID, m.Name, nullStr(m.Description), m.Status, nullStr(m.Lead), nullStr(m.StartDate), nullStr(m.EndDate), m.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "module could not be saved")
			return
		}
		writeJSON(w, http.StatusCreated, m)
	case http.MethodPut:
		var m Module
		fields, err := decodeJSONRequest(r, &m)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid module payload")
			return
		}
		if strings.TrimSpace(m.ID) == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		var existing Module
		var description, lead, startDate, endDate sqlNullString
		var created string
		err = a.DB.QueryRow(`SELECT project_id, name, description, status, lead, start_date, end_date, created_at FROM modules WHERE id=?`, m.ID).
			Scan(&existing.ProjectID, &existing.Name, &description, &existing.Status, &lead, &startDate, &endDate, &created)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "module not found")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "module could not be loaded")
			return
		}
		existing.Description, existing.Lead, existing.StartDate, existing.EndDate = description.String, lead.String, startDate.String, endDate.String
		if _, ok := fields["projectId"]; ok && m.ProjectID != existing.ProjectID {
			writeJSONError(w, http.StatusBadRequest, "projectId cannot be changed")
			return
		}
		if _, ok := fields["name"]; !ok { m.Name = existing.Name }
		if _, ok := fields["description"]; !ok { m.Description = existing.Description }
		if _, ok := fields["status"]; !ok { m.Status = existing.Status }
		if _, ok := fields["lead"]; !ok { m.Lead = existing.Lead }
		if _, ok := fields["startDate"]; !ok { m.StartDate = existing.StartDate }
		if _, ok := fields["endDate"]; !ok { m.EndDate = existing.EndDate }
		if strings.TrimSpace(m.Name) == "" {
			writeJSONError(w, http.StatusBadRequest, "name required")
			return
		}
		if (m.StartDate != "" && !validISODate(m.StartDate)) || (m.EndDate != "" && !validISODate(m.EndDate)) {
			writeJSONError(w, http.StatusBadRequest, "invalid module date")
			return
		}
		result, err := a.DB.Exec(`UPDATE modules SET name=?, description=?, status=?, lead=?, start_date=?, end_date=? WHERE id=?`,
			m.Name, nullStr(m.Description), m.Status, nullStr(m.Lead), nullStr(m.StartDate), nullStr(m.EndDate), m.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "module could not be updated")
			return
		}
		changed, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "module update could not be verified")
			return
		}
		if changed == 0 {
			writeJSONError(w, http.StatusNotFound, "module not found")
			return
		}
		m.ProjectID = existing.ProjectID
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		writeJSON(w, http.StatusOK, m)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if strings.TrimSpace(id) == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		result, err := a.DB.Exec(`DELETE FROM modules WHERE id=?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "module could not be deleted")
			return
		}
		changed, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "module deletion could not be verified")
			return
		}
		if changed == 0 {
			writeJSONError(w, http.StatusNotFound, "module not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *AppContext) handlePages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" {
			row := a.DB.QueryRow(`SELECT id, project_id, workspace, parent_id, title, icon, cover, content, created_by, created_at, updated_at, version, linked_database_id, linked_view_id FROM pages WHERE id=?`, id)
			var p Page
			var created, updated string
			var pid, parent, icon, cover, content, createdBy, linkedDB, linkedView sqlNullString
			var ws int
			var version int
			if err := row.Scan(&p.ID, &pid, &ws, &parent, &p.Title, &icon, &cover, &content, &createdBy, &created, &updated, &version, &linkedDB, &linkedView); err != nil {
				writeJSON(w, 404, map[string]string{"error": "not found"})
				return
			}
			p.ProjectID = pid.String
			p.Workspace = ws == 1
			p.ParentID = parent.String
			p.Icon = icon.String
			p.Cover = cover.String
			p.Content = content.String
			p.CreatedBy = createdBy.String
			p.CreatedAt, _ = time.Parse(time.RFC3339, created)
			p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
			p.Version = version
			p.LinkedDatabaseID = linkedDB.String
			p.LinkedViewID = linkedView.String
			writeJSON(w, 200, p)
			return
		}
		projectID := r.URL.Query().Get("projectId")
		q := `SELECT id, project_id, workspace, parent_id, title, icon, cover, created_by, created_at, updated_at, version FROM pages`
		args := []any{}
		if projectID != "" {
			q += ` WHERE project_id=?`
			args = append(args, projectID)
		} else {
			q += ` WHERE workspace=1 OR project_id IS NULL`
		}
		q += ` ORDER BY updated_at DESC`
		rows, _ := a.DB.Query(q, args...)
		defer rows.Close()
		out := []Page{}
		for rows.Next() {
			var p Page
			var created, updated string
			var pid, parent, icon, cover, createdBy sqlNullString
			var ws int
			var version int
			_ = rows.Scan(&p.ID, &pid, &ws, &parent, &p.Title, &icon, &cover, &createdBy, &created, &updated, &version)
			p.Version = version
			p.ProjectID = pid.String
			p.Workspace = ws == 1
			p.ParentID = parent.String
			p.Icon = icon.String
			p.Cover = cover.String
			p.CreatedBy = createdBy.String
			p.CreatedAt, _ = time.Parse(time.RFC3339, created)
			p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
			out = append(out, p)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var p Page
		if _, err := decodeJSONRequest(r, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid page payload"})
			return
		}
		if p.ID == "" {
			p.ID = newID("page")
		}
		if err := a.validatePageParent(p.ID, p.ParentID); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if p.Title == "" {
			p.Title = "Untitled"
		}
		if p.CreatedBy == "" {
			p.CreatedBy = "You"
		}
		now := time.Now().UTC().Truncate(time.Second)
		p.CreatedAt = now
		p.UpdatedAt = now
		ws := 0
		if p.Workspace {
			ws = 1
		}
		_, _ = a.DB.Exec(`INSERT INTO pages (id, project_id, workspace, parent_id, title, icon, cover, content, created_by, created_at, updated_at, version) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, nullStr(p.ProjectID), ws, nullStr(p.ParentID), p.Title, nullStr(p.Icon), nullStr(p.Cover), p.Content, p.CreatedBy, p.CreatedAt.Format(time.RFC3339), p.UpdatedAt.Format(time.RFC3339), 1)
		p.Version = 1
		writeJSON(w, 201, p)
	case http.MethodPut:
		var p Page
		if _, err := decodeJSONRequest(r, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid page payload"})
			return
		}
		if p.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id required"})
			return
		}
		if err := a.validatePageParent(p.ID, p.ParentID); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if p.Version <= 0 {
			writeJSON(w, 400, map[string]string{"error": "version required"})
			return
		}
		var exists int
		if err := a.DB.QueryRow(`SELECT COUNT(*) FROM pages WHERE id=?`, p.ID).Scan(&exists); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db error"})
			return
		}
		if exists == 0 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		p.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		newVersion := p.Version + 1
		ws := 0
		if p.Workspace {
			ws = 1
		}
		res, err := a.DB.Exec(`UPDATE pages SET title=?, icon=?, cover=?, content=?, project_id=?, parent_id=?, workspace=?, updated_at=?, version=? WHERE id=? AND version=?`,
			p.Title, nullStr(p.Icon), nullStr(p.Cover), p.Content, nullStr(p.ProjectID), nullStr(p.ParentID), ws, p.UpdatedAt.Format(time.RFC3339), newVersion, p.ID, p.Version)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "db error"})
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			writeJSON(w, 409, map[string]string{"error": "version conflict"})
			return
		}
		p.Version = newVersion
		writeJSON(w, 200, p)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM pages WHERE id=?`, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleComments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		entity := r.URL.Query().Get("entity")
		entityID := r.URL.Query().Get("entityId")
		rows, _ := a.DB.Query(`SELECT id, entity, entity_id, author, body, created_at FROM comments WHERE entity=? AND entity_id=? ORDER BY created_at ASC`, entity, entityID)
		defer rows.Close()
		out := []Comment{}
		for rows.Next() {
			var c Comment
			var created string
			_ = rows.Scan(&c.ID, &c.Entity, &c.EntityID, &c.Author, &c.Body, &created)
			c.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, c)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var c Comment
		if _, err := decodeJSONRequest(r, &c); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid comment payload")
			return
		}
		if strings.TrimSpace(c.Entity) == "" || strings.TrimSpace(c.EntityID) == "" || strings.TrimSpace(c.Body) == "" {
			writeJSONError(w, http.StatusBadRequest, "entity, entityId and body required")
			return
		}
		if c.ID == "" {
			c.ID = newID("comment")
		}
		if c.Author == "" {
			c.Author = "You"
		}
		c.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO comments (id, entity, entity_id, author, body, created_at) VALUES (?,?,?,?,?,?)`, c.ID, c.Entity, c.EntityID, c.Author, c.Body, c.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "db error")
			return
		}
		writeJSON(w, http.StatusCreated, c)
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		res, err := a.DB.Exec(`DELETE FROM comments WHERE id=?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "db error")
			return
		}
		affected, err := res.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "db error")
			return
		}
		if affected == 0 {
			writeJSONError(w, http.StatusNotFound, "comment not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleActivity(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		entity := r.URL.Query().Get("entity")
		entityID := r.URL.Query().Get("entityId")
		q := `SELECT id, entity, entity_id, action, actor, details, created_at FROM activity`
		args := []any{}
		if entity != "" && entityID != "" {
			q += ` WHERE entity=? AND entity_id=?`
			args = append(args, entity, entityID)
		}
		q += ` ORDER BY created_at DESC LIMIT 100`
		rows, _ := a.DB.Query(q, args...)
		defer rows.Close()
		out := []Activity{}
		for rows.Next() {
			var a Activity
			var created string
			var details sqlNullString
			_ = rows.Scan(&a.ID, &a.Entity, &a.EntityID, &a.Action, &a.Actor, &details, &created)
			a.Details = details.String
			a.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, a)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var act Activity
		if _, err := decodeJSONRequest(r, &act); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid activity payload")
			return
		}
		if strings.TrimSpace(act.Entity) == "" || strings.TrimSpace(act.EntityID) == "" || strings.TrimSpace(act.Action) == "" {
			writeJSONError(w, http.StatusBadRequest, "entity, entityId and action required")
			return
		}
		if act.ID == "" {
			act.ID = newID("activity")
		}
		if act.Actor == "" {
			act.Actor = "You"
		}
		act.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO activity (id, entity, entity_id, action, actor, details, created_at) VALUES (?,?,?,?,?,?,?)`, act.ID, act.Entity, act.EntityID, act.Action, act.Actor, nullStr(act.Details), act.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "db error")
			return
		}
		writeJSON(w, http.StatusCreated, act)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleNotifications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, _ := a.DB.Query(`SELECT id, title, body, type, read, created_at FROM notifications ORDER BY created_at DESC LIMIT 50`)
		defer rows.Close()
		out := []Notification{}
		for rows.Next() {
			var n Notification
			var created string
			var body sqlNullString
			var read int
			_ = rows.Scan(&n.ID, &n.Title, &body, &n.Type, &read, &created)
			n.Body = body.String
			n.Read = read == 1
			n.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, n)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var n Notification
		if _, err := decodeJSONRequest(r, &n); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid notification payload")
			return
		}
		if n.ID == "" {
			n.ID = newID("notif")
		}
		if n.Type == "" {
			n.Type = "info"
		}
		n.CreatedAt = time.Now().UTC().Truncate(time.Second)
		ri := 0
		if n.Read {
			ri = 1
		}
		if _, err := a.DB.Exec(`INSERT INTO notifications (id, title, body, type, read, created_at) VALUES (?,?,?,?,?,?)`, n.ID, n.Title, nullStr(n.Body), n.Type, ri, n.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "notification could not be saved")
			return
		}
		writeJSON(w, http.StatusCreated, n)
	case http.MethodPut:
		var payload struct {
			ID          string `json:"id"`
			Read        *bool  `json:"read"`
			MarkAllRead *bool  `json:"markAllRead"`
		}
		if _, err := decodeJSONRequest(r, &payload); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid notification payload")
			return
		}
		if payload.MarkAllRead != nil && *payload.MarkAllRead && payload.ID == "" && payload.Read == nil {
			if _, err := a.DB.Exec(`UPDATE notifications SET read=1`); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "notifications could not be updated")
				return
			}
		} else if payload.ID != "" && payload.Read != nil && payload.MarkAllRead == nil {
			ri := 0
			if *payload.Read {
				ri = 1
			}
			result, err := a.DB.Exec(`UPDATE notifications SET read=? WHERE id=?`, ri, payload.ID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "notification could not be updated")
				return
			}
			rows, err := result.RowsAffected()
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "notification update could not be verified")
				return
			}
			if rows == 0 {
				writeJSONError(w, http.StatusNotFound, "notification not found")
				return
			}
		} else {
			writeJSONError(w, http.StatusBadRequest, "provide id and read, or markAllRead=true")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleStickies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.DB.Query(`SELECT id, title, body, color, created_at FROM stickies ORDER BY created_at DESC`)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to list stickies")
			return
		}
		defer rows.Close()
		out := []Sticky{}
		for rows.Next() {
			var s Sticky
			var created string
			var title, body, color sqlNullString
			if err := rows.Scan(&s.ID, &title, &body, &color, &created); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "failed to read sticky")
				return
			}
			s.Title = title.String
			s.Body = body.String
			s.Color = color.String
			s.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, s)
		}
		if err := rows.Err(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to list stickies")
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var s Sticky
		if _, err := decodeJSONRequest(r, &s); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON payload")
			return
		}
		if s.ID == "" {
			s.ID = newID("sticky")
		}
		if s.Color == "" {
			s.Color = "yellow"
		}
		s.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO stickies (id, title, body, color, created_at) VALUES (?,?,?,?,?)`,
			s.ID, nullStr(s.Title), s.Body, s.Color, s.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to create sticky")
			return
		}
		writeJSON(w, http.StatusCreated, s)
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		result, err := a.DB.Exec(`DELETE FROM stickies WHERE id=?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to delete sticky")
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to verify sticky deletion")
			return
		}
		if affected == 0 {
			writeJSONError(w, http.StatusNotFound, "sticky not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *AppContext) handleDrafts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, _ := a.DB.Query(`SELECT id, kind, title, content, created_at, updated_at FROM drafts ORDER BY updated_at DESC`)
		defer rows.Close()
		out := []Draft{}
		for rows.Next() {
			var d Draft
			var created, updated string
			_ = rows.Scan(&d.ID, &d.Kind, &d.Title, &d.Content, &created, &updated)
			d.CreatedAt, _ = time.Parse(time.RFC3339, created)
			d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
			out = append(out, d)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var d Draft
		if _, err := decodeJSONRequest(r, &d); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
			return
		}
		if d.ID == "" {
			d.ID = newID("draft")
		}
		if d.Kind == "" {
			d.Kind = "note"
		}
		now := time.Now().UTC().Truncate(time.Second)
		d.CreatedAt = now
		d.UpdatedAt = now
		if _, err := a.DB.Exec(`INSERT INTO drafts (id, kind, title, content, created_at, updated_at) VALUES (?,?,?,?,?,?)`, d.ID, d.Kind, d.Title, d.Content, d.CreatedAt.Format(time.RFC3339), d.UpdatedAt.Format(time.RFC3339)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create draft"})
			return
		}
		writeJSON(w, http.StatusCreated, d)
	case http.MethodPut:
		var d Draft
		fields, err := decodeJSONRequest(r, &d)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
			return
		}
		if d.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		_, hasKind := fields["kind"]
		_, hasTitle := fields["title"]
		_, hasContent := fields["content"]
		d.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		result, err := a.DB.Exec(`UPDATE drafts SET
			kind=CASE WHEN ? THEN ? ELSE kind END,
			title=CASE WHEN ? THEN ? ELSE title END,
			content=CASE WHEN ? THEN ? ELSE content END,
			updated_at=? WHERE id=?`,
			hasKind, d.Kind, hasTitle, d.Title, hasContent, d.Content,
			d.UpdatedAt.Format(time.RFC3339), d.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update draft"})
			return
		}
		rows, err := result.RowsAffected()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify draft update"})
			return
		}
		if rows == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "draft not found"})
			return
		}
		var created string
		if err := a.DB.QueryRow(`SELECT kind, title, content, created_at FROM drafts WHERE id=?`, d.ID).Scan(&d.Kind, &d.Title, &d.Content, &created); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read updated draft"})
			return
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, created)
		writeJSON(w, http.StatusOK, d)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM drafts WHERE id=?`, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleViews(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := r.URL.Query().Get("projectId")
		q := `SELECT id, project_id, name, filters, created_at FROM views`
		args := []any{}
		if projectID != "" {
			q += ` WHERE project_id=?`
			args = append(args, projectID)
		}
		q += ` ORDER BY created_at DESC`
		rows, err := a.DB.Query(q, args...)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to list views")
			return
		}
		defer rows.Close()
		out := []View{}
		for rows.Next() {
			var v View
			var created string
			var pid, filters sqlNullString
			if err := rows.Scan(&v.ID, &pid, &v.Name, &filters, &created); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "failed to read view")
				return
			}
			v.ProjectID = pid.String
			v.Filters = filters.String
			v.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to list views")
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var v View
		if _, err := decodeJSONRequest(r, &v); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON payload")
			return
		}
		if strings.TrimSpace(v.Name) == "" {
			writeJSONError(w, http.StatusBadRequest, "name required")
			return
		}
		if v.ID == "" {
			v.ID = newID("view")
		}
		v.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO views (id, project_id, name, filters, created_at) VALUES (?,?,?,?,?)`,
			v.ID, nullStr(v.ProjectID), v.Name, v.Filters, v.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to create view")
			return
		}
		writeJSON(w, http.StatusCreated, v)
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		result, err := a.DB.Exec(`DELETE FROM views WHERE id=?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to delete view")
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to verify view deletion")
			return
		}
		if affected == 0 {
			writeJSONError(w, http.StatusNotFound, "view not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *AppContext) handleIntake(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := r.URL.Query().Get("projectId")
		q := `SELECT id, project_id, title, body, status, created_at FROM intake`
		args := []any{}
		if projectID != "" {
			q += ` WHERE project_id=?`
			args = append(args, projectID)
		}
		q += ` ORDER BY created_at DESC`
		rows, err := a.DB.Query(q, args...)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to list intake"})
			return
		}
		defer rows.Close()
		out := []IntakeItem{}
		for rows.Next() {
			var it IntakeItem
			var created string
			var body sqlNullString
			if err := rows.Scan(&it.ID, &it.ProjectID, &it.Title, &body, &it.Status, &created); err != nil {
				writeJSON(w, 500, map[string]string{"error": "failed to read intake"})
				return
			}
			it.Body = body.String
			it.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, it)
		}
		if err := rows.Err(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to list intake"})
			return
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var it IntakeItem
		if _, err := decodeJSONRequest(r, &it); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid intake payload"})
			return
		}
		if strings.TrimSpace(it.ProjectID) == "" || strings.TrimSpace(it.Title) == "" {
			writeJSON(w, 400, map[string]string{"error": "projectId and title required"})
			return
		}
		if it.ID == "" {
			it.ID = newID("intake")
		}
		if it.Status == "" {
			it.Status = "pending"
		}
		if strings.TrimSpace(it.Status) == "" {
			writeJSON(w, 400, map[string]string{"error": "status cannot be blank"})
			return
		}
		it.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO intake (id, project_id, title, body, status, created_at) VALUES (?,?,?,?,?,?)`,
			it.ID, it.ProjectID, it.Title, nullStr(it.Body), it.Status, it.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to create intake"})
			return
		}
		writeJSON(w, 201, it)
	case http.MethodPut:
		var it IntakeItem
		if _, err := decodeJSONRequest(r, &it); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid intake payload"})
			return
		}
		if it.ID == "" || strings.TrimSpace(it.Status) == "" {
			writeJSON(w, 400, map[string]string{"error": "id and status required"})
			return
		}
		result, err := a.DB.Exec(`UPDATE intake SET status=? WHERE id=?`, it.Status, it.ID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to update intake"})
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to verify intake update"})
			return
		}
		if affected == 0 {
			writeJSON(w, 404, map[string]string{"error": "intake item not found"})
			return
		}
		writeJSON(w, 200, it)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, 400, map[string]string{"error": "id required"})
			return
		}
		result, err := a.DB.Exec(`DELETE FROM intake WHERE id=?`, id)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to delete intake"})
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to verify intake deletion"})
			return
		}
		if affected == 0 {
			writeJSON(w, 404, map[string]string{"error": "intake item not found"})
			return
		}
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := map[string]any{"projects": []Project{}, "tasks": []Task{}, "pages": []Page{}}
	if q == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	like := "%" + q + "%"
	projects, err := func() ([]Project, error) {
		rows, err := a.DB.Query(`SELECT id, name, description, status, owner, color, created_at FROM projects WHERE name LIKE ? OR description LIKE ?`, like, like)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := []Project{}
		for rows.Next() {
			var p Project
			var created string
			var description, owner, color sqlNullString
			if err := rows.Scan(&p.ID, &p.Name, &description, &p.Status, &owner, &color, &created); err != nil {
				return nil, err
			}
			p.Description = description.String
			p.Owner = owner.String
			p.Color = color.String
			p.CreatedAt, _ = time.Parse(time.RFC3339, created)
			result = append(result, p)
		}
		return result, rows.Err()
	}()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to search projects"})
		return
	}
	tasks, err := func() ([]Task, error) {
		rows, err := a.DB.Query(`SELECT id, project_id, title, description, status, priority, due_date, assignee, created_at FROM tasks WHERE title LIKE ? OR description LIKE ?`, like, like)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := []Task{}
		for rows.Next() {
			var t Task
			var created string
			var desc, due, assignee sqlNullString
			if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &desc, &t.Status, &t.Priority, &due, &assignee, &created); err != nil {
				return nil, err
			}
			t.Description = desc.String
			t.DueDate = due.String
			t.Assignee = assignee.String
			t.CreatedAt, _ = time.Parse(time.RFC3339, created)
			result = append(result, t)
		}
		return result, rows.Err()
	}()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to search tasks"})
		return
	}
	pages, err := func() ([]Page, error) {
		rows, err := a.DB.Query(`SELECT id, project_id, workspace, parent_id, title, icon, cover, content, created_by, created_at, updated_at FROM pages WHERE title LIKE ? OR content LIKE ?`, like, like)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := []Page{}
		for rows.Next() {
			var p Page
			var created, updated string
			var pid, parent, icon, cover, content, createdBy sqlNullString
			var ws int
			if err := rows.Scan(&p.ID, &pid, &ws, &parent, &p.Title, &icon, &cover, &content, &createdBy, &created, &updated); err != nil {
				return nil, err
			}
			p.ProjectID = pid.String
			p.Workspace = ws == 1
			p.ParentID = parent.String
			p.Icon = icon.String
			p.Cover = cover.String
			p.Content = content.String
			p.CreatedBy = createdBy.String
			p.CreatedAt, _ = time.Parse(time.RFC3339, created)
			p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
			result = append(result, p)
		}
		return result, rows.Err()
	}()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to search pages"})
		return
	}
	out["projects"] = projects
	out["tasks"] = tasks
	out["pages"] = pages
	writeJSON(w, http.StatusOK, out)
}

func (a *AppContext) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	projectID := r.URL.Query().Get("projectId")
	counts := map[string]int{}
	q := `SELECT status, COUNT(*) FROM tasks`
	args := []any{}
	if projectID != "" {
		q += ` WHERE project_id=?`
		args = append(args, projectID)
	}
	q += ` GROUP BY status`
	rows, err := a.DB.Query(q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load task analytics"})
		return
	}
	for rows.Next() {
		var st string
		var c int
		if err := rows.Scan(&st, &c); err != nil {
			rows.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read task analytics"})
			return
		}
		counts[st] = c
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load task analytics"})
		return
	}
	if err := rows.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to close task analytics query"})
		return
	}
	totalProjects, totalTasks, totalPages, totalCycles, totalModules := 0, 0, 0, 0, 0
	for _, metric := range []struct {
		query string
		value *int
	}{
		{`SELECT COUNT(*) FROM projects`, &totalProjects},
		{`SELECT COUNT(*) FROM tasks`, &totalTasks},
		{`SELECT COUNT(*) FROM pages`, &totalPages},
		{`SELECT COUNT(*) FROM cycles`, &totalCycles},
		{`SELECT COUNT(*) FROM modules`, &totalModules},
	} {
		if err := a.DB.QueryRow(metric.query).Scan(metric.value); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load analytics totals"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"taskStatusCounts": counts,
		"totalProjects":    totalProjects,
		"totalTasks":       totalTasks,
		"totalPages":       totalPages,
		"totalCycles":      totalCycles,
		"totalModules":     totalModules,
	})
}

func (a *AppContext) handlePageMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var payload struct {
		ID       string `json:"id"`
		ParentID string `json:"parentId"`
		Version  int    `json:"version"`
	}
	if _, err := decodeJSONRequest(r, &payload); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid page move payload"})
		return
	}
	if payload.ID == "" {
		writeJSON(w, 400, map[string]string{"error": "id required"})
		return
	}
	if payload.Version <= 0 {
		writeJSON(w, 400, map[string]string{"error": "version required"})
		return
	}
	newVersion, err := a.movePageAtomic(payload.ID, payload.ParentID, payload.Version)
	if err != nil {
		msg := err.Error()
		switch msg {
		case "not found", "parent not found":
			writeJSON(w, 404, map[string]string{"error": msg})
		case "version conflict":
			writeJSON(w, 409, map[string]string{"error": msg})
		case "self parent", "circular":
			writeJSON(w, 400, map[string]string{"error": msg})
		default:
			writeJSON(w, 500, map[string]string{"error": msg})
		}
		return
	}
	writeJSON(w, 200, map[string]any{"id": payload.ID, "parentId": payload.ParentID, "version": newVersion})
}

func (a *AppContext) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.DB.Query(`SELECT id, name, slug, description, created_at FROM workspaces ORDER BY created_at DESC`)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to list workspaces"})
			return
		}
		out := []Workspace{}
		for rows.Next() {
			var ws Workspace
			var created string
			var desc, slug sqlNullString
			if err := rows.Scan(&ws.ID, &ws.Name, &slug, &desc, &created); err != nil {
				rows.Close()
				writeJSON(w, 500, map[string]string{"error": "failed to read workspace"})
				return
			}
			ws.Slug = slug.String
			ws.Description = desc.String
			ws.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, ws)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeJSON(w, 500, map[string]string{"error": "failed to list workspaces"})
			return
		}
		if err := rows.Close(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to close workspace query"})
			return
		}
		if len(out) == 0 {
			ws := Workspace{ID: "ws-default", Name: "Workspace", Slug: "workspace", CreatedAt: time.Now().UTC().Truncate(time.Second)}
			if _, err := a.DB.Exec(`INSERT OR IGNORE INTO workspaces (id, name, slug, created_at) VALUES (?,?,?,?)`,
				ws.ID, ws.Name, ws.Slug, ws.CreatedAt.Format(time.RFC3339)); err != nil {
				writeJSON(w, 500, map[string]string{"error": "failed to initialize workspace"})
				return
			}
			var created string
			var slug, desc sqlNullString
			if err := a.DB.QueryRow(`SELECT id, name, slug, description, created_at FROM workspaces WHERE id=?`, ws.ID).
				Scan(&ws.ID, &ws.Name, &slug, &desc, &created); err != nil {
				writeJSON(w, 500, map[string]string{"error": "failed to load workspace"})
				return
			}
			ws.Slug = slug.String
			ws.Description = desc.String
			ws.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, ws)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var ws Workspace
		if _, err := decodeJSONRequest(r, &ws); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid workspace payload"})
			return
		}
		if ws.ID == "" {
			ws.ID = newID("ws")
		}
		if strings.TrimSpace(ws.Name) == "" {
			ws.Name = "Workspace"
		}
		ws.CreatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := a.DB.Exec(`INSERT INTO workspaces (id, name, slug, description, created_at) VALUES (?,?,?,?,?)`,
			ws.ID, ws.Name, nullStr(ws.Slug), nullStr(ws.Description), ws.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to create workspace"})
			return
		}
		writeJSON(w, 201, ws)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rows, err := a.DB.Query(`SELECT id, name, email, role, created_at FROM members ORDER BY created_at DESC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list members"})
		return
	}
	out := []Member{}
	for rows.Next() {
		var m Member
		var created string
		var email sqlNullString
		if err := rows.Scan(&m.ID, &m.Name, &email, &m.Role, &created); err != nil {
			rows.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read member"})
			return
		}
		m.Email = email.String
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list members"})
		return
	}
	if err := rows.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to close member query"})
		return
	}
	if len(out) == 0 {
		m := Member{ID: "mem-you", Name: "You", Role: "owner", CreatedAt: time.Now().UTC().Truncate(time.Second)}
		if _, err := a.DB.Exec(`INSERT OR IGNORE INTO members (id, name, role, created_at) VALUES (?,?,?,?)`,
			m.ID, m.Name, m.Role, m.CreatedAt.Format(time.RFC3339)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to initialize member"})
			return
		}
		var created string
		var email sqlNullString
		if err := a.DB.QueryRow(`SELECT id, name, email, role, created_at FROM members WHERE id=?`, m.ID).
			Scan(&m.ID, &m.Name, &email, &m.Role, &created); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load default member"})
			return
		}
		m.Email = email.String
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

// sqlNullString helper
type sqlNullString struct {
	String string
	Valid  bool
}

func (s *sqlNullString) Scan(value any) error {
	if value == nil {
		s.String = ""
		s.Valid = false
		return nil
	}
	switch v := value.(type) {
	case string:
		s.String = v
	case []byte:
		s.String = string(v)
	}
	s.Valid = true
	return nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ensure sort import used
var _ = sort.Strings
