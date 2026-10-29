package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// Library metadata lives in app_state, alongside the portable SQLite workspace.
// Separate keys prevent a recent-visit write from overwriting a favorite toggle.
func (a *AppContext) handlePageLibrary(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.DB.Query(`SELECT p.id,
   EXISTS(SELECT 1 FROM app_state WHERE key='favorite-page:' || p.id AND value='1'),
   COALESCE((SELECT value FROM app_state WHERE key='recent-page:' || p.id),'')
   FROM pages p ORDER BY 3 DESC, p.id`)
		if err != nil {
			writeJSONError(w, 500, "Page library could not be loaded")
			return
		}
		defer rows.Close()
		favorites, recent := []string{}, []string{}
		for rows.Next() {
			var id, opened string
			var favorite bool
			if err := rows.Scan(&id, &favorite, &opened); err != nil {
				writeJSONError(w, 500, "Page library could not be read")
				return
			}
			if favorite {
				favorites = append(favorites, id)
			}
			if opened != "" && len(recent) < 12 {
				recent = append(recent, id)
			}
		}
		if rows.Err() != nil {
			writeJSONError(w, 500, "Page library could not be read")
			return
		}
		writeJSON(w, 200, map[string][]string{"favorites": favorites, "recent": recent})
	case http.MethodPost:
		var body struct {
			ID       string `json:"id"`
			Favorite *bool  `json:"favorite"`
			Visit    bool   `json:"visit"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil || body.ID == "" || (body.Favorite == nil && !body.Visit) || (body.Favorite != nil && body.Visit) {
			writeJSONError(w, 400, "Supply a page id and either favorite or visit")
			return
		}
		key, value := "recent-page:"+body.ID, time.Now().UTC().Format(time.RFC3339Nano)
		if body.Favorite != nil {
			key = "favorite-page:" + body.ID
			value = "0"
			if *body.Favorite {
				value = "1"
			}
		}
		result, err := a.DB.Exec(`INSERT INTO app_state(key,value) SELECT ?,? WHERE EXISTS(SELECT 1 FROM pages WHERE id=?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value, body.ID)
		if err != nil {
			writeJSONError(w, 500, "Page library could not be saved")
			return
		}
		count, err := result.RowsAffected()
		if err != nil {
			writeJSONError(w, 500, "Page library could not be saved")
			return
		}
		if count == 0 {
			writeJSONError(w, 404, "Page not found")
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
