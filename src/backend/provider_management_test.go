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

func TestProviderManagementRevisionAndCredentialLifecycle(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	call := func(method string, value any) *httptest.ResponseRecorder {
		var body bytes.Buffer
		if value != nil {
			if err := json.NewEncoder(&body).Encode(value); err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, "/api/provider-management", &body)
		w := httptest.NewRecorder()
		a.handleProviderManagement(w, r)
		return w
	}
	read := func() providerSnapshot {
		t.Helper()
		w := call(http.MethodGet, nil)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var snap providerSnapshot
		if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
			t.Fatal(err)
		}
		return snap
	}
	initial := read()
	input := providerInput{ID: "mock", Label: "Mock", Provider: "openai-compatible", BaseURL: "http://127.0.0.1:11434/v1", Model: "model", MaxTokens: 128, Enabled: true, APIKey: "synthetic-secret"}
	original := providerInput{ID: "default", Label: "Local Test", Provider: "openai-compatible", BaseURL: "http://127.0.0.1:11434/v1", Model: "test-model", MaxTokens: 0}
	w := call(http.MethodPut, providerUpdate{Revision: initial.Revision, Providers: []providerInput{original, input}})
	if w.Code != 200 || strings.Contains(w.Body.String(), input.APIKey) {
		t.Fatalf("save failed or disclosed secret: %d %s", w.Code, w.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(a.RootPath, "config", "providers.json"))
	if err != nil || bytes.Contains(raw, []byte(input.APIKey)) {
		t.Fatal("credential was written unprotected")
	}
	current := read()
	input.APIKey = ""
	input.Label = "Renamed"
	w = call(http.MethodPut, providerUpdate{Revision: initial.Revision, Providers: []providerInput{original, input}})
	if w.Code != 409 {
		t.Fatalf("stale write: %d", w.Code)
	}
	after, _ := os.ReadFile(filepath.Join(a.RootPath, "config", "providers.json"))
	if !bytes.Equal(raw, after) {
		t.Fatal("stale write changed provider bytes")
	}
	w = call(http.MethodPut, providerUpdate{Revision: current.Revision, Providers: []providerInput{original, input}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	providers, _, err := loadProviderFile(a.RootPath)
	if err != nil || len(providers) != 2 || providers[1].APIKey != "synthetic-secret" {
		t.Fatal("existing credential not retained")
	}
	current = read()
	input.ClearAPIKey = true
	w = call(http.MethodPut, providerUpdate{Revision: current.Revision, Providers: []providerInput{original, input}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	providers, _, err = loadProviderFile(a.RootPath)
	if err != nil || providers[1].APIKey != "" {
		t.Fatal("clear credential failed")
	}
	current = read()
	w = call(http.MethodPut, providerUpdate{Revision: current.Revision, Providers: []providerInput{original}})
	if w.Code != 200 || len(read().Providers) != 1 {
		t.Fatal("delete failed")
	}
}
func TestProviderManagementRejectsInvalidInputsAndMissingPing(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	before, err := os.ReadFile(filepath.Join(a.RootPath, "config", "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{"revision":"x","providers":[]}`, `{"revision":"x","revision":"y","providers":[]}`, `{"revision":"x","providers":[],"extra":1}`} {
		w := httptest.NewRecorder()
		a.handleProviderManagement(w, httptest.NewRequest(http.MethodPut, "/api/provider-management", strings.NewReader(payload)))
		if w.Code != 400 {
			t.Fatalf("invalid accepted: %d", w.Code)
		}
	}
	after, _ := os.ReadFile(filepath.Join(a.RootPath, "config", "providers.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("invalid input changed config")
	}
	w := httptest.NewRecorder()
	a.handleAIPing(w, httptest.NewRequest(http.MethodPost, "/api/ai/test-connection", strings.NewReader(`{"providerId":"missing"}`)))
	if w.Code != 404 {
		t.Fatalf("missing id must not test another provider: %d", w.Code)
	}
}
