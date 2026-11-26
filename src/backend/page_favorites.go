package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// recentPagesLimit caps the number of recently viewed page IDs retained.
const recentPagesLimit = 20

// loadIDList reads a JSON string-array stored under an app_state key. A missing
// or empty entry yields an empty slice rather than an error.
func (a *AppContext) loadIDList(key string) ([]string, error) {
	var value string
	err := a.DB.QueryRow(`SELECT value FROM app_state WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(value) == "" {
		return []string{}, nil
	}
	ids := []string{}
	if err := json.Unmarshal([]byte(value), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// saveIDList persists a JSON string-array under an app_state key.
func (a *AppContext) saveIDList(key string, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = a.DB.Exec(`INSERT OR REPLACE INTO app_state (key, value) VALUES (?, ?)`, key, string(data))
	return err
}

// pageExists reports whether a page row exists.
func (a *AppContext) pageExists(id string) (bool, error) {
	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM pages WHERE id=?`, id).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func containsID(ids []string, id string) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

// handlePageFavorites manages the ordered set of favorited page IDs.
func (a *AppContext) handlePageFavorites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ids, err := a.loadIDList("favorites")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "favorites could not be loaded")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ids": ids})
	case http.MethodPost:
		var payload struct {
			ID string `json:"id"`
		}
		if _, err := decodeJSONRequest(r, &payload); err != nil || strings.TrimSpace(payload.ID) == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		ok, err := a.pageExists(payload.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "page could not be verified")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "page not found")
			return
		}
		ids, err := a.loadIDList("favorites")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "favorites could not be loaded")
			return
		}
		if !containsID(ids, payload.ID) {
			ids = append([]string{payload.ID}, ids...)
			if err := a.saveIDList("favorites", ids); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "favorites could not be saved")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ids": ids})
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		ids, err := a.loadIDList("favorites")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "favorites could not be loaded")
			return
		}
		filtered := ids[:0]
		for _, existing := range ids {
			if existing != id {
				filtered = append(filtered, existing)
			}
		}
		if err := a.saveIDList("favorites", filtered); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "favorites could not be saved")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ids": filtered})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handlePageRecent manages the most-recently-viewed page IDs (newest first).
func (a *AppContext) handlePageRecent(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ids, err := a.loadIDList("recent")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "recent pages could not be loaded")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ids": ids})
	case http.MethodPost:
		var payload struct {
			ID string `json:"id"`
		}
		if _, err := decodeJSONRequest(r, &payload); err != nil || strings.TrimSpace(payload.ID) == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		ok, err := a.pageExists(payload.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "page could not be verified")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "page not found")
			return
		}
		ids, err := a.loadIDList("recent")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "recent pages could not be loaded")
			return
		}
		updated := make([]string, 0, len(ids)+1)
		updated = append(updated, payload.ID)
		for _, existing := range ids {
			if existing != payload.ID {
				updated = append(updated, existing)
			}
		}
		if len(updated) > recentPagesLimit {
			updated = updated[:recentPagesLimit]
		}
		if err := a.saveIDList("recent", updated); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "recent pages could not be saved")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ids": updated})
	case http.MethodDelete:
		// Clear the entire recent list.
		if err := a.saveIDList("recent", []string{}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "recent pages could not be cleared")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ids": []string{}})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}