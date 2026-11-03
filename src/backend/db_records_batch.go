package main

import (
	"fmt"
	"net/http"
	"time"
)

// maxDBRecordBatch bounds one request so a single body and transaction stay
// responsive (the JSON body limit is 4 MiB). Larger imports send several batches.
const maxDBRecordBatch = 1000

type dbRecordCreate struct {
	ID     string         `json:"id,omitempty"`
	Title  string         `json:"title,omitempty"`
	Values map[string]any `json:"values,omitempty"`
}

// handleDBRecordsBatch creates records and their backing pages in one SQLite
// transaction. Per-record POST remains unchanged for interactive clients; this
// endpoint exists for imports where thousands of HTTP round trips and one FULL
// synchronous commit per row dominate. Any invalid row, duplicate/colliding ID
// or failed readback rolls the entire batch back, so a failed import never
// leaves a partially written table.
func (a *AppContext) handleDBRecordsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DatabaseID string           `json:"databaseId"`
		Records    []dbRecordCreate `json:"records"`
	}
	if _, err := decodeJSONRequest(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request JSON"})
		return
	}
	if req.DatabaseID == "" || len(req.Records) == 0 || len(req.Records) > maxDBRecordBatch {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("databaseId and 1-%d records required", maxDBRecordBatch),
		})
		return
	}
	seen := make(map[string]bool, len(req.Records))
	for _, rec := range req.Records {
		if rec.ID != "" {
			if seen[rec.ID] {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "duplicate record id in batch"})
				return
			}
			seen[rec.ID] = true
		}
	}

	tx, err := a.DB.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not begin record batch"})
		return
	}
	defer tx.Rollback()

	var active int
	if err := tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL", req.DatabaseID).Scan(&active); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not inspect database"})
		return
	}
	if active != 1 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "database not found"})
		return
	}

	definitions, err := loadDBRecordPropertyDefinitions(tx, req.DatabaseID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read property definitions"})
		return
	}

	// Client-supplied IDs let an importer retry a batch. A second attempt must
	// conflict instead of silently appending a duplicate copy.
	for _, rec := range req.Records {
		if rec.ID == "" {
			continue
		}
		var existing int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM db_records WHERE id=?`, rec.ID).Scan(&existing); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not inspect record ids"})
			return
		}
		if existing > 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("record %s already exists", rec.ID)})
			return
		}
	}

	pageStmt, err := tx.Prepare(`INSERT INTO pages (id, title, icon, content, workspace, created_by, version) VALUES (?,?,?,?,1,'You',1)`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not prepare batch pages"})
		return
	}
	defer pageStmt.Close()
	recordStmt, err := tx.Prepare(`INSERT INTO db_records (id, database_id, title, page_id, version, created_at, updated_at) VALUES (?,?,?,?,1,?,?)`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not prepare batch records"})
		return
	}
	defer recordStmt.Close()
	valueStmt, err := tx.Prepare(`INSERT INTO db_values (record_id, property_id, value, version, updated_at) VALUES (?,?,?,1,?)`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not prepare batch values"})
		return
	}
	defer valueStmt.Close()

	// One timestamp for the batch keeps commits cheap; keyset pagination
	// breaks ties by the time-ordered record id.
	now := time.Now().UTC().Format(time.RFC3339)
	ids := make([]string, 0, len(req.Records))
	for index, rec := range req.Records {
		id := rec.ID
		if id == "" {
			id = newID("rec")
		}
		title := rec.Title
		if title == "" {
			title = "Untitled"
		}
		pageID := newID("page")
		if _, err := pageStmt.Exec(pageID, title, "", ""); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("could not create backing page for record %d", index)})
			return
		}
		if _, err := recordStmt.Exec(id, req.DatabaseID, title, pageID, now, now); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("could not create record %d", index)})
			return
		}
		if status, err := insertRecordValuesWithDefinitions(tx, valueStmt, id, rec.Values, now, definitions); err != nil {
			writeJSON(w, status, map[string]string{"error": fmt.Sprintf("record %d: %v", index, err)})
			return
		}
		ids = append(ids, id)
	}

	// Materialize every record and page from the transaction before publishing
	// the batch. A damaged schema or trigger that hides a committed row must
	// roll back the batch instead of reporting success.
	for index, id := range ids {
		var recordID, databaseID, title, pageID, pageTitle string
		var version, pageVersion int
		err := tx.QueryRow(`SELECT r.id, r.database_id, r.title, r.page_id,
			r.version, p.title, p.version
			FROM db_records r JOIN pages p ON p.id=r.page_id
			WHERE r.id=? AND r.database_id=? AND r.deleted_at IS NULL`, id, req.DatabaseID).
			Scan(&recordID, &databaseID, &title, &pageID, &version, &pageTitle, &pageVersion)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("could not read created record %d", index)})
			return
		}
		if pageID == "" || version <= 0 || pageVersion <= 0 || pageTitle != title {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("created record %d and backing page are inconsistent", index)})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"created": len(ids),
		"ids":     ids,
	})
}
