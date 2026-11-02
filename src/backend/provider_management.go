package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Serialize both legacy whole-list writes and the revision-checked UI endpoint.
var providerSettingsMu sync.Mutex

type providerSnapshot struct {
	Revision  string        `json:"revision"`
	Providers []providerDTO `json:"providers"`
}
type providerUpdate struct {
	Revision  string          `json:"revision"`
	Providers []providerInput `json:"providers"`
}

func (a *AppContext) readProviderSnapshot() (providerSnapshot, []AIProvider, error) {
	raw, err := os.ReadFile(filepath.Join(a.RootPath, "config", "providers.json"))
	if err != nil {
		return providerSnapshot{}, nil, err
	}
	providers, _, err := loadProviderFile(a.RootPath)
	if err != nil {
		return providerSnapshot{}, nil, err
	}
	digest := sha256.Sum256(raw)
	return providerSnapshot{Revision: hex.EncodeToString(digest[:]), Providers: toProviderDTOs(providers)}, providers, nil
}
func (a *AppContext) handleProviderManagement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		writeJSONError(w, 405, "method not allowed")
		return
	}
	var update providerUpdate
	if r.Method == http.MethodPut {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 131073))
		if err != nil || len(raw) > 131072 || rejectDuplicateJSONKeys(raw) != nil {
			writeJSONError(w, 400, "invalid provider settings")
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&update) != nil || decoder.Decode(new(any)) != io.EOF || len(update.Revision) != 64 || update.Providers == nil || len(update.Providers) > 100 {
			writeJSONError(w, 400, "invalid provider settings")
			return
		}
	}
	providerSettingsMu.Lock()
	defer providerSettingsMu.Unlock()
	snapshot, current, err := a.readProviderSnapshot()
	if err != nil {
		writeJSONError(w, 500, "provider configuration could not be read")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, snapshot)
		return
	}
	if update.Revision != snapshot.Revision {
		writeJSONError(w, 409, "Provider settings changed in another window. Reload before saving.")
		return
	}
	existing := make(map[string]AIProvider, len(current))
	for _, p := range current {
		existing[p.ID] = p
	}
	seen := make(map[string]bool)
	next := make([]AIProvider, 0, len(update.Providers))
	for _, v := range update.Providers {
		id := strings.TrimSpace(v.ID)
		label := strings.TrimSpace(v.Label)
		model := strings.TrimSpace(v.Model)
		if id == "" || id != v.ID || len(id) > 128 || seen[id] {
			writeJSONError(w, 400, "Invalid or duplicate provider ID.")
			return
		}
		seen[id] = true
		old, exists := existing[id]
		// Untouched older entries can contain values accepted by the legacy API.
		// Preserve them while editing another provider rather than forcing a migration.
		if exists && v.APIKey == "" && !v.ClearAPIKey && v.Label == old.Label && v.Provider == old.Provider && v.BaseURL == old.BaseURL && v.Model == old.Model && v.Temperature == old.Temperature && v.MaxTokens == old.MaxTokens && v.Enabled == old.Enabled && v.Streaming == old.Streaming && v.ToolCalling == old.ToolCalling {
			next = append(next, old)
			continue
		}
		if len(label) == 0 || len(label) > 200 || len(model) == 0 || len(model) > 256 || len(v.APIKey) > 8192 || len(v.BaseURL) > 2048 || (v.ClearAPIKey && v.APIKey != "") || (v.Provider != "openai-compatible" && (!exists || v.Provider != old.Provider)) || v.MaxTokens < 1 || v.MaxTokens > 131072 || v.Temperature < 0 || v.Temperature > 2 || validateProviderURL(v.BaseURL) != nil {
			writeJSONError(w, 400, "Invalid provider settings. Check the name, model, URL and numeric limits.")
			return
		}
		key := v.APIKey
		if key == "" && !v.ClearAPIKey {
			key = existing[id].APIKey
		}
		next = append(next, AIProvider{ID: id, Label: label, Provider: v.Provider, BaseURL: v.BaseURL, APIKey: key, Model: model, Temperature: v.Temperature, MaxTokens: v.MaxTokens, Enabled: v.Enabled, Streaming: v.Streaming, ToolCalling: v.ToolCalling})
	}
	if err := writeProviders(a.RootPath, next); err != nil {
		writeJSONError(w, 500, "provider configuration could not be saved")
		return
	}
	snapshot, _, err = a.readProviderSnapshot()
	if err != nil {
		writeJSONError(w, 500, "Settings were saved but could not be reloaded. Reload before retrying.")
		return
	}
	writeJSON(w, 200, snapshot)
}
