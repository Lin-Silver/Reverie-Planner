package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPageFavoritesAndRecentPersistence verifies the favorites and recent
// page lists persist in app_state, reject unknown pages, dedupe, order
// newest-first and survive a database reopen.
func TestPageFavoritesAndRecentPersistence(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	create := func(title string) string {
		res := httptest.NewRecorder()
		app.handlePages(res, httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title":"`+title+`"}`)))
		if res.Code != http.StatusCreated {
			t.Fatalf("create page %q: %d %s", title, res.Code, res.Body.String())
		}
		var p Page
		if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	pageA := create("Alpha")
	pageB := create("Beta")

	call := func(method, path, body string, h http.HandlerFunc) map[string]any {
		res := httptest.NewRecorder()
		var reader *strings.Reader
		if body != "" {
			reader = strings.NewReader(body)
		} else {
			reader = strings.NewReader("")
		}
		h.ServeHTTP(res, httptest.NewRequest(method, path, reader))
		if res.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, res.Code, res.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
		return out
	}
	idsOf := func(m map[string]any) []string {
		raw, _ := m["ids"].([]any)
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			out = append(out, v.(string))
		}
		return out
	}

	// Favorite A then B; newest favorite should be first.
	call(http.MethodPost, "/api/pages/favorites", `{"id":"`+pageA+`"}`, app.handlePageFavorites)
	got := idsOf(call(http.MethodPost, "/api/pages/favorites", `{"id":"`+pageB+`"}`, app.handlePageFavorites))
	if len(got) != 2 || got[0] != pageB || got[1] != pageA {
		t.Fatalf("favorites order: %v", got)
	}
	// Re-favoriting A must not duplicate.
	got = idsOf(call(http.MethodPost, "/api/pages/favorites", `{"id":"`+pageA+`"}`, app.handlePageFavorites))
	if len(got) != 2 {
		t.Fatalf("favorites dedupe failed: %v", got)
	}
	// Unknown page is rejected with 404.
	res := httptest.NewRecorder()
	app.handlePageFavorites(res, httptest.NewRequest(http.MethodPost, "/api/pages/favorites", strings.NewReader(`{"id":"missing"}`)))
	if res.Code != http.StatusNotFound {
		t.Fatalf("favorite unknown page: %d", res.Code)
	}
	// Remove B.
	got = idsOf(call(http.MethodDelete, "/api/pages/favorites?id="+pageB, "", app.handlePageFavorites))
	if len(got) != 1 || got[0] != pageA {
		t.Fatalf("favorites removal: %v", got)
	}

	// Recent: view B then A then B again -> B first, capped and deduped.
	call(http.MethodPost, "/api/pages/recent", `{"id":"`+pageB+`"}`, app.handlePageRecent)
	call(http.MethodPost, "/api/pages/recent", `{"id":"`+pageA+`"}`, app.handlePageRecent)
	got = idsOf(call(http.MethodPost, "/api/pages/recent", `{"id":"`+pageB+`"}`, app.handlePageRecent))
	if len(got) != 2 || got[0] != pageB || got[1] != pageA {
		t.Fatalf("recent order/dedupe: %v", got)
	}

	// Persists across reopen.
	if err := app.DB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := initializeDatabase(app.RootPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	app.DB = reopened
	got = idsOf(call(http.MethodGet, "/api/pages/favorites", "", app.handlePageFavorites))
	if len(got) != 1 || got[0] != pageA {
		t.Fatalf("favorites after reopen: %v", got)
	}
	got = idsOf(call(http.MethodGet, "/api/pages/recent", "", app.handlePageRecent))
	if len(got) != 2 || got[0] != pageB {
		t.Fatalf("recent after reopen: %v", got)
	}

	// Clearing recent works.
	got = idsOf(call(http.MethodDelete, "/api/pages/recent", "", app.handlePageRecent))
	if len(got) != 0 {
		t.Fatalf("recent clear: %v", got)
	}
}

// TestRecentPagesCap verifies the recent list never exceeds recentPagesLimit.
func TestRecentPagesCap(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	var ids []string
	for i := 0; i < recentPagesLimit+5; i++ {
		res := httptest.NewRecorder()
		app.handlePages(res, httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title":"p"}`)))
		var p Page
		_ = json.Unmarshal(res.Body.Bytes(), &p)
		ids = append(ids, p.ID)
	}
	for _, id := range ids {
		res := httptest.NewRecorder()
		app.handlePageRecent(res, httptest.NewRequest(http.MethodPost, "/api/pages/recent", strings.NewReader(`{"id":"`+id+`"}`)))
		if res.Code != http.StatusOK {
			t.Fatalf("recent post: %d", res.Code)
		}
	}
	res := httptest.NewRecorder()
	app.handlePageRecent(res, httptest.NewRequest(http.MethodGet, "/api/pages/recent", nil))
	var out map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	raw := out["ids"].([]any)
	if len(raw) != recentPagesLimit {
		t.Fatalf("recent cap: got %d want %d", len(raw), recentPagesLimit)
	}
	// Newest id must be first.
	if raw[0].(string) != ids[len(ids)-1] {
		t.Fatalf("recent newest-first broken: %v", raw[0])
	}
}