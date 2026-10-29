package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func stopPortableService(root string) error {
	// Read only: attempting to stop must not create a new workspace.
	var cfg AppConfig
	raw, err := os.ReadFile(filepath.Join(root, "config", "app.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	if !isLoopbackHost(cfg.Host) || cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("invalid local service configuration")
	}
	var metadata serviceMetadata
	raw, err = os.ReadFile(filepath.Join(root, "runtime", "service.json"))
	if err != nil {
		return fmt.Errorf("no owned running service: %w", err)
	}
	if err = json.Unmarshal(raw, &metadata); err != nil || len(metadata.Token) == 0 {
		return fmt.Errorf("invalid service metadata")
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))+"/api/shutdown", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+metadata.Token)
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("stop refused: HTTP %d", res.StatusCode)
	}
	return nil
}

func (a *AppContext) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"version": appVersion, "root": a.RootPath,
		"database":    filepath.Join(a.RootPath, "data", "planner.db"),
		"backups":     filepath.Join(a.RootPath, "data", "backups"),
		"attachments": filepath.Join(a.RootPath, "data", "attachments"),
	})
}
