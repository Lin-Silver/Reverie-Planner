package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFileOrFail returns the named file under root, failing the test on error.
func readFileOrFail(t *testing.T, root string, parts ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return data
}

// These regressions cover the three handlers that previously bypassed the
// normalized decoder (handleSettings, handleProviderSettings, handleAIPing).
// They must reject duplicate keys and trailing values, enforce the expected
// top-level JSON shape, and leave configuration bytes untouched on rejection.

func TestSettingsDecodeNormalization(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	do := func(body string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		app.handleSettings(res, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body)))
		return res
	}

	bad := []struct{ name, body string }{
		{"top level duplicate", `{"theme":"dark","theme":"light"}`},
		{"nested duplicate", `{"theme":"dark","extra":{"a":1,"a":2}}`},
		{"trailing object", `{"theme":"dark"}{"theme":"light"}`},
		{"top level array", `[{"theme":"dark"}]`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			before := readFileOrFail(t, app.RootPath, "config", "app.json")
			res := do(tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400, got %d: %s", res.Code, res.Body.String())
			}
			after := readFileOrFail(t, app.RootPath, "config", "app.json")
			if !bytes.Equal(before, after) {
				t.Fatalf("app.json changed after rejected payload")
			}
		})
	}

	// A well-formed object still persists the theme change.
	res := do(`{"theme":"light"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("valid settings update failed: %d %s", res.Code, res.Body.String())
	}
	var cfg AppConfig
	if err := json.Unmarshal(readFileOrFail(t, app.RootPath, "config", "app.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "light" {
		t.Fatalf("theme not persisted: got %q", cfg.Theme)
	}
}

func TestProviderSettingsDecodeNormalization(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	do := func(body string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		app.handleProviderSettings(res, httptest.NewRequest(http.MethodPost, "/api/ai/providers", strings.NewReader(body)))
		return res
	}

	bad := []struct{ name, body string }{
		{"duplicate key in element", `[{"id":"p1","label":"L","provider":"openai-compatible","baseURL":"http://127.0.0.1:11434/v1","model":"m","model":"n"}]`},
		{"trailing array", `[{"id":"p1","label":"L","provider":"openai-compatible","baseURL":"http://127.0.0.1:11434/v1","model":"m"}][{"id":"p2"}]`},
		{"top level object", `{"id":"p1","label":"L","provider":"openai-compatible","baseURL":"http://127.0.0.1:11434/v1","model":"m"}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			before := readFileOrFail(t, app.RootPath, "config", "providers.json")
			res := do(tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400, got %d: %s", res.Code, res.Body.String())
			}
			after := readFileOrFail(t, app.RootPath, "config", "providers.json")
			if !bytes.Equal(before, after) {
				t.Fatalf("providers.json changed after rejected payload")
			}
		})
	}

	// A well-formed array persists and is returned as DTOs.
	res := do(`[{"id":"p1","label":"Mock","provider":"openai-compatible","baseURL":"http://127.0.0.1:11434/v1","model":"mock-model"}]`)
	if res.Code != http.StatusOK {
		t.Fatalf("valid provider update failed: %d %s", res.Code, res.Body.String())
	}
	var dtos []providerDTO
	if err := json.Unmarshal(res.Body.Bytes(), &dtos); err != nil {
		t.Fatal(err)
	}
	if len(dtos) != 1 || dtos[0].ID != "p1" {
		t.Fatalf("unexpected provider DTOs: %+v", dtos)
	}
}

func TestAIPingDecodeNormalization(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	do := func(body string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		app.handleAIPing(res, httptest.NewRequest(http.MethodPost, "/api/ai/test-connection", strings.NewReader(body)))
		return res
	}

	bad := []struct{ name, body string }{
		{"top level duplicate", `{"providerId":"a","providerId":"b"}`},
		{"trailing object", `{"providerId":"a"}{"providerId":"b"}`},
		{"top level array", `[{"providerId":"a"}]`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			before := readFileOrFail(t, app.RootPath, "config", "providers.json")
			res := do(tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400, got %d: %s", res.Code, res.Body.String())
			}
			after := readFileOrFail(t, app.RootPath, "config", "providers.json")
			if !bytes.Equal(before, after) {
				t.Fatalf("providers.json changed after rejected payload")
			}
		})
	}

	// A well-formed payload passes decoding and reaches connectivity; use a
	// local simulated provider so no external network is required.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	body := `{"provider":{"id":"p1","label":"Mock","provider":"openai-compatible","baseURL":"` + server.URL + `/v1","model":"mock-model"}}`
	res := do(body)
	if res.Code != http.StatusOK {
		t.Fatalf("valid connection test failed: %d %s", res.Code, res.Body.String())
	}
}