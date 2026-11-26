package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Template is a reusable page blueprint stored locally in app_state.
type Template struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Icon    string `json:"icon"`
	Content string `json:"content"`
}

func (a *AppContext) loadTemplates() ([]Template, error) {
	var value string
	err := a.DB.QueryRow(`SELECT value FROM app_state WHERE key='templates'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && strings.TrimSpace(value) == "") {
		return []Template{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Template{}
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *AppContext) saveTemplates(list []Template) error {
	if list == nil {
		list = []Template{}
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	_, err = a.DB.Exec(`INSERT OR REPLACE INTO app_state (key, value) VALUES ('templates', ?)`, string(data))
	return err
}

func (a *AppContext) handleTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := a.loadTemplates()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "templates could not be loaded")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"templates": list})
	case http.MethodPost:
		var tpl Template
		if _, err := decodeJSONRequest(r, &tpl); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid template payload")
			return
		}
		if strings.TrimSpace(tpl.Name) == "" {
			writeJSONError(w, http.StatusBadRequest, "name required")
			return
		}
		if tpl.ID == "" {
			tpl.ID = newID("tpl")
		}
		list, err := a.loadTemplates()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "templates could not be loaded")
			return
		}
		replaced := false
		for i := range list {
			if list[i].ID == tpl.ID {
				list[i] = tpl
				replaced = true
				break
			}
		}
		if !replaced {
			list = append(list, tpl)
		}
		if err := a.saveTemplates(list); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "template could not be saved")
			return
		}
		writeJSON(w, http.StatusCreated, tpl)
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "id required")
			return
		}
		list, err := a.loadTemplates()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "templates could not be loaded")
			return
		}
		filtered := list[:0]
		for _, tpl := range list {
			if tpl.ID != id {
				filtered = append(filtered, tpl)
			}
		}
		if err := a.saveTemplates(filtered); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "template could not be deleted")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"templates": filtered})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}