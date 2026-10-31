package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const appearanceKey = "workspace-appearance:v1"

type workspaceAppearance struct {
	Style     string `json:"style"`
	Wallpaper string `json:"wallpaper"`
	Material  string `json:"material"`
	Version   int    `json:"version"`
}

func defaultAppearance() workspaceAppearance {
	return workspaceAppearance{Style: "glass", Wallpaper: "sakura", Material: "balanced"}
}
func validateAppearance(p workspaceAppearance) error {
	if (p.Style != "glass" && p.Style != "classic") || (p.Wallpaper != "sakura" && p.Wallpaper != "mist") || (p.Material != "soft" && p.Material != "balanced" && p.Material != "frosted") || p.Version < 0 {
		return errors.New("unsupported appearance settings")
	}
	return nil
}
func readAppearance(query interface{ QueryRow(string, ...any) *sql.Row }) (workspaceAppearance, string, error) {
	p := defaultAppearance()
	var raw string
	err := query.QueryRow(`SELECT value FROM app_state WHERE key=?`, appearanceKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, "", nil
	}
	if err != nil {
		return p, "", err
	}
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return p, raw, err
	}
	return p, raw, validateAppearance(p)
}
func (a *AppContext) handleAppearance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		p, _, err := readAppearance(a.DB)
		if err != nil {
			writeJSONError(w, 500, "Appearance could not be loaded")
			return
		}
		writeJSON(w, 200, p)
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		writeJSONError(w, 405, "method not allowed")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(raw) > 4096 || rejectDuplicateJSONKeys(raw) != nil {
		writeJSONError(w, 400, "invalid appearance JSON")
		return
	}
	var request workspaceAppearance
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || validateAppearance(request) != nil {
		writeJSONError(w, 400, "invalid appearance settings")
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSONError(w, 500, "Appearance could not be saved")
		return
	}
	defer tx.Rollback()
	current, previous, err := readAppearance(tx)
	if err != nil {
		writeJSONError(w, 500, "Appearance could not be read")
		return
	}
	if current.Version != request.Version {
		writeJSONError(w, 409, "Appearance changed in another window. Reload appearance before retrying.")
		return
	}
	if current.Style == request.Style && current.Wallpaper == request.Wallpaper && current.Material == request.Material {
		writeJSON(w, 200, current)
		return
	}
	request.Version++
	encoded, err := json.Marshal(request)
	if err != nil {
		writeJSONError(w, 500, "Appearance could not be saved")
		return
	}
	if previous == "" {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO app_state(key,value) VALUES(?,?)`, appearanceKey, string(encoded))
	} else {
		var result sql.Result
		result, err = tx.ExecContext(r.Context(), `UPDATE app_state SET value=? WHERE key=? AND value=?`, string(encoded), appearanceKey, previous)
		if err == nil {
			count, e := result.RowsAffected()
			if e != nil || count != 1 {
				writeJSONError(w, 409, "Appearance changed. Reload appearance before retrying.")
				return
			}
		}
	}
	if err != nil {
		writeJSONError(w, 500, "Appearance could not be saved")
		return
	}
	saved, _, err := readAppearance(tx)
	if err != nil || saved != request {
		writeJSONError(w, 500, "Appearance readback failed")
		return
	}
	if tx.Commit() != nil {
		writeJSONError(w, 500, "Appearance could not be committed")
		return
	}
	writeJSON(w, 200, saved)
}
