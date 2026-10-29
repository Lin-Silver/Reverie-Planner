package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationMutationsRejectInvalidJSONAndPreserveData(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		app.handleNotifications(res, httptest.NewRequest(method, "/api/notifications", strings.NewReader(body)))
		return res
	}
	create := func(title string) Notification {
		t.Helper()
		res := request(http.MethodPost, fmt.Sprintf(`{"title":%q,"body":"Keep body"}`, title))
		if res.Code != http.StatusCreated {
			t.Fatalf("create notification: %d %s", res.Code, res.Body.String())
		}
		var n Notification
		if err := json.Unmarshal(res.Body.Bytes(), &n); err != nil || n.ID == "" {
			t.Fatalf("decode notification: %+v err=%v", n, err)
		}
		return n
	}
	first := create("Keep first")
	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providerBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}

	checkUnchanged := func(t *testing.T) {
		t.Helper()
		var count int
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("notification count changed: %d err=%v", count, err)
		}
		var title, body string
		var read int
		if err := app.DB.QueryRow(`SELECT title, body, read FROM notifications WHERE id=?`, first.ID).Scan(&title, &body, &read); err != nil {
			t.Fatal(err)
		}
		if title != first.Title || body != "Keep body" || read != 0 {
			t.Fatalf("rejected mutation changed notification: title=%q body=%q read=%d", title, body, read)
		}
		after, err := os.ReadFile(providerPath)
		if err != nil || !bytes.Equal(providerBefore, after) {
			t.Fatalf("provider configuration changed: %v", err)
		}
	}
	invalid := []struct {
		name, method, body string
	}{
		{"post trailing", http.MethodPost, `{"title":"Bad"}{"title":"Other"}`},
		{"post duplicate", http.MethodPost, `{"title":"Bad","title":"Other"}`},
		{"post null", http.MethodPost, `null`},
		{"post oversized", http.MethodPost, fmt.Sprintf(`{"title":%q}`, strings.Repeat("x", 4<<20))},
		{"put trailing", http.MethodPut, fmt.Sprintf(`{"id":%q,"read":true}{"read":false}`, first.ID)},
		{"put duplicate", http.MethodPut, fmt.Sprintf(`{"id":%q,"read":true,"read":false}`, first.ID)},
		{"put malformed", http.MethodPut, `{"markAllRead":true`},
		{"put array", http.MethodPut, `[{"markAllRead":true}]`},
		{"put null", http.MethodPut, `null`},
		{"put wrong type", http.MethodPut, fmt.Sprintf(`{"id":%q,"read":"yes"}`, first.ID)},
		{"put missing operation", http.MethodPut, `{}`},
		{"put false mark all", http.MethodPut, `{"markAllRead":false}`},
		{"put conflicting actions", http.MethodPut, fmt.Sprintf(`{"id":%q,"read":true,"markAllRead":true}`, first.ID)},
		{"put oversized", http.MethodPut, fmt.Sprintf(`{"id":%q,"read":true,"padding":%q}`, first.ID, strings.Repeat("x", 4<<20))},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.method, tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
			}
			checkUnchanged(t)
		})
	}

	missing := request(http.MethodPut, `{"id":"missing-notification","read":true}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing notification must return 404, got %d: %s", missing.Code, missing.Body.String())
	}
	checkUnchanged(t)

	duplicate := request(http.MethodPost, fmt.Sprintf(`{"id":%q,"title":"Duplicate"}`, first.ID))
	if duplicate.Code != http.StatusInternalServerError {
		t.Fatalf("duplicate ID must report insert failure, got %d: %s", duplicate.Code, duplicate.Body.String())
	}
	checkUnchanged(t)

	markOne := request(http.MethodPut, fmt.Sprintf(`{"id":%q,"read":true}`, first.ID))
	if markOne.Code != http.StatusOK {
		t.Fatalf("mark one: %d %s", markOne.Code, markOne.Body.String())
	}
	var read int
	if err := app.DB.QueryRow(`SELECT read FROM notifications WHERE id=?`, first.ID).Scan(&read); err != nil || read != 1 {
		t.Fatalf("mark one not persisted: read=%d err=%v", read, err)
	}
	clearOne := request(http.MethodPut, fmt.Sprintf(`{"id":%q,"read":false}`, first.ID))
	if clearOne.Code != http.StatusOK {
		t.Fatalf("clear one: %d %s", clearOne.Code, clearOne.Body.String())
	}
	second := create("Keep second")
	markAll := request(http.MethodPut, `{"markAllRead":true}`)
	if markAll.Code != http.StatusOK {
		t.Fatalf("mark all: %d %s", markAll.Code, markAll.Body.String())
	}
	for _, id := range []string{first.ID, second.ID} {
		if err := app.DB.QueryRow(`SELECT read FROM notifications WHERE id=?`, id).Scan(&read); err != nil || read != 1 {
			t.Fatalf("mark all not persisted for %s: read=%d err=%v", id, read, err)
		}
	}
	after, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providerBefore, after) {
		t.Fatalf("valid notification mutations changed provider configuration: %v", err)
	}
}
