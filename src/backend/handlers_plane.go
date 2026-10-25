package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func newID(prefix string) string {
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
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
		_ = json.NewDecoder(r.Body).Decode(&l)
		if l.ID == "" {
			l.ID = newID("label")
		}
		if l.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		if l.Color == "" {
			l.Color = "gray"
		}
		l.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO labels (id, project_id, name, color, created_at) VALUES (?,?,?,?,?)`, l.ID, nullStr(l.ProjectID), l.Name, l.Color, l.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, l)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM labels WHERE id=?`, id)
		w.WriteHeader(204)
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
		_ = json.NewDecoder(r.Body).Decode(&s)
		if s.ID == "" {
			s.ID = newID("state")
		}
		if s.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		if s.Color == "" {
			s.Color = "gray"
		}
		if s.Group == "" {
			s.Group = "backlog"
		}
		s.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO states (id, project_id, name, color, group_name, sequence, created_at) VALUES (?,?,?,?,?,?,?)`, s.ID, s.ProjectID, s.Name, s.Color, s.Group, s.Sequence, s.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, s)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM states WHERE id=?`, id)
		w.WriteHeader(204)
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
		_ = json.NewDecoder(r.Body).Decode(&c)
		if c.ID == "" {
			c.ID = newID("cycle")
		}
		if c.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		if c.Status == "" {
			c.Status = "draft"
		}
		c.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO cycles (id, project_id, name, description, start_date, end_date, status, created_at) VALUES (?,?,?,?,?,?,?,?)`, c.ID, c.ProjectID, c.Name, nullStr(c.Description), nullStr(c.StartDate), nullStr(c.EndDate), c.Status, c.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, c)
	case http.MethodPut:
		var c Cycle
		_ = json.NewDecoder(r.Body).Decode(&c)
		if c.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id required"})
			return
		}
		_, _ = a.DB.Exec(`UPDATE cycles SET name=?, description=?, start_date=?, end_date=?, status=? WHERE id=?`, c.Name, nullStr(c.Description), nullStr(c.StartDate), nullStr(c.EndDate), c.Status, c.ID)
		writeJSON(w, 200, c)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM cycles WHERE id=?`, id)
		w.WriteHeader(204)
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
		rows, _ := a.DB.Query(q, args...)
		defer rows.Close()
		out := []Module{}
		for rows.Next() {
			var m Module
			var created string
			var desc, lead, start, end sqlNullString
			_ = rows.Scan(&m.ID, &m.ProjectID, &m.Name, &desc, &m.Status, &lead, &start, &end, &created)
			m.Description = desc.String
			m.Lead = lead.String
			m.StartDate = start.String
			m.EndDate = end.String
			m.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, m)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var m Module
		_ = json.NewDecoder(r.Body).Decode(&m)
		if m.ID == "" {
			m.ID = newID("module")
		}
		if m.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		if m.Status == "" {
			m.Status = "backlog"
		}
		m.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO modules (id, project_id, name, description, status, lead, start_date, end_date, created_at) VALUES (?,?,?,?,?,?,?,?,?)`, m.ID, m.ProjectID, m.Name, nullStr(m.Description), m.Status, nullStr(m.Lead), nullStr(m.StartDate), nullStr(m.EndDate), m.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, m)
	case http.MethodPut:
		var m Module
		_ = json.NewDecoder(r.Body).Decode(&m)
		if m.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id required"})
			return
		}
		_, _ = a.DB.Exec(`UPDATE modules SET name=?, description=?, status=?, lead=?, start_date=?, end_date=? WHERE id=?`, m.Name, nullStr(m.Description), m.Status, nullStr(m.Lead), nullStr(m.StartDate), nullStr(m.EndDate), m.ID)
		writeJSON(w, 200, m)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM modules WHERE id=?`, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handlePages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" {
			row := a.DB.QueryRow(`SELECT id, project_id, workspace, parent_id, title, icon, cover, content, created_by, created_at, updated_at, version FROM pages WHERE id=?`, id)
			var p Page
			var created, updated string
			var pid, parent, icon, cover, content, createdBy sqlNullString
			var ws int
			var version int
			if err := row.Scan(&p.ID, &pid, &ws, &parent, &p.Title, &icon, &cover, &content, &createdBy, &created, &updated, &version); err != nil {
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
		_ = json.NewDecoder(r.Body).Decode(&p)
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
		_ = json.NewDecoder(r.Body).Decode(&p)
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
		_ = json.NewDecoder(r.Body).Decode(&c)
		if c.ID == "" {
			c.ID = newID("comment")
		}
		if c.Author == "" {
			c.Author = "You"
		}
		c.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO comments (id, entity, entity_id, author, body, created_at) VALUES (?,?,?,?,?,?)`, c.ID, c.Entity, c.EntityID, c.Author, c.Body, c.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, c)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM comments WHERE id=?`, id)
		w.WriteHeader(204)
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
		_ = json.NewDecoder(r.Body).Decode(&act)
		if act.ID == "" {
			act.ID = newID("activity")
		}
		if act.Actor == "" {
			act.Actor = "You"
		}
		act.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO activity (id, entity, entity_id, action, actor, details, created_at) VALUES (?,?,?,?,?,?,?)`, act.ID, act.Entity, act.EntityID, act.Action, act.Actor, nullStr(act.Details), act.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, act)
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
		_ = json.NewDecoder(r.Body).Decode(&n)
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
		_, _ = a.DB.Exec(`INSERT INTO notifications (id, title, body, type, read, created_at) VALUES (?,?,?,?,?,?)`, n.ID, n.Title, nullStr(n.Body), n.Type, ri, n.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, n)
	case http.MethodPut:
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if id, ok := payload["id"].(string); ok {
			if read, ok := payload["read"].(bool); ok {
				ri := 0
				if read {
					ri = 1
				}
				_, _ = a.DB.Exec(`UPDATE notifications SET read=? WHERE id=?`, ri, id)
			}
		}
		if all, ok := payload["markAllRead"].(bool); ok && all {
			_, _ = a.DB.Exec(`UPDATE notifications SET read=1`)
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleStickies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, _ := a.DB.Query(`SELECT id, title, body, color, created_at FROM stickies ORDER BY created_at DESC`)
		defer rows.Close()
		out := []Sticky{}
		for rows.Next() {
			var s Sticky
			var created string
			var title, body, color sqlNullString
			_ = rows.Scan(&s.ID, &title, &body, &color, &created)
			s.Title = title.String
			s.Body = body.String
			s.Color = color.String
			s.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, s)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var s Sticky
		_ = json.NewDecoder(r.Body).Decode(&s)
		if s.ID == "" {
			s.ID = newID("sticky")
		}
		if s.Color == "" {
			s.Color = "yellow"
		}
		s.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO stickies (id, title, body, color, created_at) VALUES (?,?,?,?,?)`, s.ID, nullStr(s.Title), s.Body, s.Color, s.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, s)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM stickies WHERE id=?`, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
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
		_ = json.NewDecoder(r.Body).Decode(&d)
		if d.ID == "" {
			d.ID = newID("draft")
		}
		if d.Kind == "" {
			d.Kind = "note"
		}
		now := time.Now().UTC().Truncate(time.Second)
		d.CreatedAt = now
		d.UpdatedAt = now
		_, _ = a.DB.Exec(`INSERT INTO drafts (id, kind, title, content, created_at, updated_at) VALUES (?,?,?,?,?,?)`, d.ID, d.Kind, d.Title, d.Content, d.CreatedAt.Format(time.RFC3339), d.UpdatedAt.Format(time.RFC3339))
		writeJSON(w, 201, d)
	case http.MethodPut:
		var d Draft
		_ = json.NewDecoder(r.Body).Decode(&d)
		if d.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id required"})
			return
		}
		d.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`UPDATE drafts SET kind=?, title=?, content=?, updated_at=? WHERE id=?`, d.Kind, d.Title, d.Content, d.UpdatedAt.Format(time.RFC3339), d.ID)
		writeJSON(w, 200, d)
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
		rows, _ := a.DB.Query(q, args...)
		defer rows.Close()
		out := []View{}
		for rows.Next() {
			var v View
			var created string
			var pid, filters sqlNullString
			_ = rows.Scan(&v.ID, &pid, &v.Name, &filters, &created)
			v.ProjectID = pid.String
			v.Filters = filters.String
			v.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, v)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var v View
		_ = json.NewDecoder(r.Body).Decode(&v)
		if v.ID == "" {
			v.ID = newID("view")
		}
		if v.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		v.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO views (id, project_id, name, filters, created_at) VALUES (?,?,?,?,?)`, v.ID, nullStr(v.ProjectID), v.Name, v.Filters, v.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, v)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM views WHERE id=?`, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
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
		rows, _ := a.DB.Query(q, args...)
		defer rows.Close()
		out := []IntakeItem{}
		for rows.Next() {
			var it IntakeItem
			var created string
			var body sqlNullString
			_ = rows.Scan(&it.ID, &it.ProjectID, &it.Title, &body, &it.Status, &created)
			it.Body = body.String
			it.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, it)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var it IntakeItem
		_ = json.NewDecoder(r.Body).Decode(&it)
		if it.ID == "" {
			it.ID = newID("intake")
		}
		if it.Status == "" {
			it.Status = "pending"
		}
		it.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO intake (id, project_id, title, body, status, created_at) VALUES (?,?,?,?,?,?)`, it.ID, it.ProjectID, it.Title, nullStr(it.Body), it.Status, it.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, it)
	case http.MethodPut:
		var it IntakeItem
		_ = json.NewDecoder(r.Body).Decode(&it)
		if it.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id required"})
			return
		}
		_, _ = a.DB.Exec(`UPDATE intake SET status=? WHERE id=?`, it.Status, it.ID)
		writeJSON(w, 200, it)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		_, _ = a.DB.Exec(`DELETE FROM intake WHERE id=?`, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := map[string]any{"projects": []Project{}, "tasks": []Task{}, "pages": []Page{}}
	if q == "" {
		writeJSON(w, 200, out)
		return
	}
	like := "%" + q + "%"
	prows, _ := a.DB.Query(`SELECT id, name, description, status, owner, color, created_at FROM projects WHERE name LIKE ? OR description LIKE ?`, like, like)
	defer prows.Close()
	projects := []Project{}
	for prows.Next() {
		var p Project
		var created string
		_ = prows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Owner, &p.Color, &created)
		p.CreatedAt, _ = time.Parse(time.RFC3339, created)
		projects = append(projects, p)
	}
	trows, _ := a.DB.Query(`SELECT id, project_id, title, description, status, priority, due_date, assignee, created_at FROM tasks WHERE title LIKE ? OR description LIKE ?`, like, like)
	defer trows.Close()
	tasks := []Task{}
	for trows.Next() {
		var t Task
		var created string
		var desc, due, assignee sqlNullString
		_ = trows.Scan(&t.ID, &t.ProjectID, &t.Title, &desc, &t.Status, &t.Priority, &due, &assignee, &created)
		t.Description = desc.String
		t.DueDate = due.String
		t.Assignee = assignee.String
		t.CreatedAt, _ = time.Parse(time.RFC3339, created)
		tasks = append(tasks, t)
	}
	pgrows, _ := a.DB.Query(`SELECT id, project_id, workspace, parent_id, title, icon, cover, content, created_by, created_at, updated_at FROM pages WHERE title LIKE ? OR content LIKE ?`, like, like)
	defer pgrows.Close()
	pages := []Page{}
	for pgrows.Next() {
		var p Page
		var created, updated string
		var pid, parent, icon, cover, content, createdBy sqlNullString
		var ws int
		_ = pgrows.Scan(&p.ID, &pid, &ws, &parent, &p.Title, &icon, &cover, &content, &createdBy, &created, &updated)
		p.ProjectID = pid.String
		p.Workspace = ws == 1
		p.ParentID = parent.String
		p.Icon = icon.String
		p.Cover = cover.String
		p.Content = content.String
		p.CreatedBy = createdBy.String
		p.CreatedAt, _ = time.Parse(time.RFC3339, created)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		pages = append(pages, p)
	}
	out["projects"] = projects
	out["tasks"] = tasks
	out["pages"] = pages
	writeJSON(w, 200, out)
}

func (a *AppContext) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	counts := map[string]int{}
	q := `SELECT status, COUNT(*) FROM tasks`
	args := []any{}
	if projectID != "" {
		q += ` WHERE project_id=?`
		args = append(args, projectID)
	}
	q += ` GROUP BY status`
	rows, _ := a.DB.Query(q, args...)
	defer rows.Close()
	for rows.Next() {
		var st string
		var c int
		_ = rows.Scan(&st, &c)
		counts[st] = c
	}
	totalProjects, totalTasks, totalPages, totalCycles, totalModules := 0, 0, 0, 0, 0
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&totalProjects)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&totalTasks)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM pages`).Scan(&totalPages)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM cycles`).Scan(&totalCycles)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM modules`).Scan(&totalModules)
	writeJSON(w, 200, map[string]any{
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
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.ID == "" {
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
		rows, _ := a.DB.Query(`SELECT id, name, slug, description, created_at FROM workspaces ORDER BY created_at DESC`)
		defer rows.Close()
		out := []Workspace{}
		for rows.Next() {
			var ws Workspace
			var created string
			var desc, slug sqlNullString
			_ = rows.Scan(&ws.ID, &ws.Name, &slug, &desc, &created)
			ws.Slug = slug.String
			ws.Description = desc.String
			ws.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, ws)
		}
		if len(out) == 0 {
			ws2 := Workspace{ID: "ws-default", Name: "Workspace", Slug: "workspace", CreatedAt: time.Now().UTC().Truncate(time.Second)}
			_, _ = a.DB.Exec(`INSERT INTO workspaces (id, name, slug, created_at) VALUES (?,?,?,?)`, ws2.ID, ws2.Name, ws2.Slug, ws2.CreatedAt.Format(time.RFC3339))
			out = append(out, ws2)
		}
		writeJSON(w, 200, out)
	case http.MethodPost:
		var ws Workspace
		_ = json.NewDecoder(r.Body).Decode(&ws)
		if ws.ID == "" {
			ws.ID = newID("ws")
		}
		if ws.Name == "" {
			ws.Name = "Workspace"
		}
		ws.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, _ = a.DB.Exec(`INSERT INTO workspaces (id, name, slug, description, created_at) VALUES (?,?,?,?,?)`, ws.ID, ws.Name, nullStr(ws.Slug), nullStr(ws.Description), ws.CreatedAt.Format(time.RFC3339))
		writeJSON(w, 201, ws)
	default:
		w.WriteHeader(405)
	}
}

func (a *AppContext) handleMembers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, _ := a.DB.Query(`SELECT id, name, email, role, created_at FROM members ORDER BY created_at DESC`)
		defer rows.Close()
		out := []Member{}
		for rows.Next() {
			var m Member
			var created string
			var email sqlNullString
			_ = rows.Scan(&m.ID, &m.Name, &email, &m.Role, &created)
			m.Email = email.String
			m.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, m)
		}
		if len(out) == 0 {
			m := Member{ID: "mem-you", Name: "You", Role: "owner", CreatedAt: time.Now().UTC().Truncate(time.Second)}
			_, _ = a.DB.Exec(`INSERT INTO members (id, name, role, created_at) VALUES (?,?,?,?)`, m.ID, m.Name, m.Role, m.CreatedAt.Format(time.RFC3339))
			out = append(out, m)
		}
		writeJSON(w, 200, out)
	default:
		w.WriteHeader(405)
	}
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
