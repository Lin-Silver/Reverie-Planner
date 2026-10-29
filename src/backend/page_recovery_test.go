package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func recoveryCall(t *testing.T, a *AppContext, method, path string, payload any, handler http.HandlerFunc, want int) *httptest.ResponseRecorder {
	t.Helper()
	var body string
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = string(b)
	}
	res := httptest.NewRecorder()
	handler(res, httptest.NewRequest(method, path, strings.NewReader(body)))
	if res.Code != want {
		t.Fatalf("%s %s: want %d got %d %s", method, path, want, res.Code, res.Body.String())
	}
	return res
}
func recoveryCreate(t *testing.T, a *AppContext, p Page) Page {
	t.Helper()
	res := recoveryCall(t, a, "POST", "/api/pages", p, a.handlePages, 201)
	if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPageHistoryRestoreConflictsRollbackAndRestart(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	parent := recoveryCreate(t, a, Page{Title: "Parent", Workspace: true})
	p := recoveryCreate(t, a, Page{Title: "Original", Content: "original writing", Icon: "star", Cover: "cover", Workspace: true})
	original := p
	p.Title = "New writing"
	p.Content = "keep newer writing"
	p.ParentID = parent.ID
	recoveryCall(t, a, "PUT", "/api/pages", p, a.handlePages, 200)
	p, err := fetchPage(a, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	list := recoveryCall(t, a, "GET", "/api/page-history?pageId="+p.ID, nil, a.handlePageHistory, 200)
	var revisions []pageRecoveryItem
	if err = json.Unmarshal(list.Body.Bytes(), &revisions); err != nil || len(revisions) != 1 || revisions[0].Version != 1 {
		t.Fatalf("history: %s %v", list.Body.String(), err)
	}
	payload := map[string]any{"pageId": p.ID, "revisionId": revisions[0].ID, "version": 1}
	recoveryCall(t, a, "POST", "/api/page-history", payload, a.handlePageHistory, 409)
	current, err := fetchPage(a, p.ID)
	if err != nil || current.Content != p.Content || current.Version != 2 {
		t.Fatalf("conflict changed page %+v %v", current, err)
	}
	payload["version"] = 2
	if _, err = a.DB.Exec("CREATE TRIGGER reject_revision BEFORE INSERT ON page_revisions BEGIN SELECT RAISE(ABORT,'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "POST", "/api/page-history", payload, a.handlePageHistory, 500)
	current, _ = fetchPage(a, p.ID)
	if current.Content != p.Content || current.Version != 2 {
		t.Fatal("history write failure did not roll back page")
	}
	if _, err = a.DB.Exec("DROP TRIGGER reject_revision"); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "POST", "/api/page-history", payload, a.handlePageHistory, 200)
	current, err = fetchPage(a, p.ID)
	if err != nil || current.Content != original.Content || current.Title != original.Title || current.ParentID != parent.ID || current.Version != 3 {
		t.Fatalf("restore lost fields/current location %+v %v", current, err)
	}
	if err = a.DB.Close(); err != nil {
		t.Fatal(err)
	}
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	list = recoveryCall(t, a, "GET", "/api/page-history?pageId="+p.ID, nil, a.handlePageHistory, 200)
	if err = json.Unmarshal(list.Body.Bytes(), &revisions); err != nil || len(revisions) != 2 {
		t.Fatalf("history lost after restart: %s %v", list.Body.String(), err)
	}
	if revisions[0].Version != 2 {
		t.Fatal("restore did not preserve newer revision")
	}
	payload["revisionId"] = revisions[0].ID
	payload["version"] = 3
	recoveryCall(t, a, "POST", "/api/page-history", payload, a.handlePageHistory, 200)
	current, _ = fetchPage(a, p.ID)
	if current.Content != p.Content || current.Title != p.Title || current.Version != 4 {
		t.Fatalf("cannot undo restore %+v", current)
	}
	// A revision belonging to another page is never a valid restore source.
	payload["pageId"] = parent.ID
	payload["version"] = 1
	recoveryCall(t, a, "POST", "/api/page-history", payload, a.handlePageHistory, 404)
	recoveryCall(t, a, "POST", "/api/page-history", map[string]any{"pageId": p.ID, "revisionId": revisions[0].ID}, a.handlePageHistory, 400)
}
func TestPageTrashParentOrderStaleDeleteAndRestart(t *testing.T) {
	a := setupTestContext(t)
	defer func() { a.DB.Close() }()
	parent := recoveryCreate(t, a, Page{Title: "Parent", Workspace: true})
	child := recoveryCreate(t, a, Page{Title: "Child", Content: "retained blocks", Workspace: true, ParentID: parent.ID})
	path := func(p Page) string { return fmt.Sprintf("/api/pages?id=%s&version=%d", p.ID, p.Version) }
	recoveryCall(t, a, "DELETE", path(parent), nil, a.handlePages, 409)
	recoveryCall(t, a, "DELETE", "/api/pages?id="+child.ID+"&version=99", nil, a.handlePages, 409)
	recoveryCall(t, a, "DELETE", "/api/pages?id="+child.ID, nil, a.handlePages, 400)
	if _, err := a.DB.Exec("CREATE TRIGGER reject_trash BEFORE INSERT ON page_trash BEGIN SELECT RAISE(ABORT,'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "DELETE", path(child), nil, a.handlePages, 500)
	if got, err := fetchPage(a, child.ID); err != nil || got.Content != child.Content {
		t.Fatal("trash write failure lost original page")
	}
	if _, err := a.DB.Exec("DROP TRIGGER reject_trash"); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "DELETE", path(child), nil, a.handlePages, 204)
	recoveryCall(t, a, "DELETE", path(parent), nil, a.handlePages, 204)
	recoveryCall(t, a, "GET", "/api/pages?id="+child.ID, nil, a.handlePages, 404)
	if err := a.DB.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	list := recoveryCall(t, a, "GET", "/api/page-trash", nil, a.handlePageTrash, 200)
	if !strings.Contains(list.Body.String(), child.ID) || !strings.Contains(list.Body.String(), parent.ID) {
		t.Fatal("restart lost trash")
	}
	reserved := child
	reserved.ParentID = ""
	recoveryCall(t, a, "POST", "/api/pages", reserved, a.handlePages, 500) // tombstone reserves identity
	payload := map[string]any{"id": child.ID, "version": child.Version}
	recoveryCall(t, a, "POST", "/api/page-trash", payload, a.handlePageTrash, 409)
	recoveryCall(t, a, "POST", "/api/page-trash", map[string]any{"id": parent.ID, "version": parent.Version}, a.handlePageTrash, 200)
	if _, err = a.DB.Exec("CREATE TRIGGER reject_restore BEFORE INSERT ON pages BEGIN SELECT RAISE(ABORT,'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "POST", "/api/page-trash", payload, a.handlePageTrash, 500)
	var count int
	if err = a.DB.QueryRow("SELECT COUNT(*) FROM page_trash WHERE id=?", child.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed restore lost tombstone")
	}
	if _, err = a.DB.Exec("DROP TRIGGER reject_restore"); err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "POST", "/api/page-trash", payload, a.handlePageTrash, 200)
	got, err := fetchPage(a, child.ID)
	if err != nil || got.ParentID != parent.ID || got.Content != child.Content || got.Version != 2 {
		t.Fatalf("restore lost child %+v %v", got, err)
	}
	recoveryCall(t, a, "POST", "/api/page-trash", payload, a.handlePageTrash, 404)
	// A pre-delete editor cannot overwrite the recovered page.
	recoveryCall(t, a, "PUT", "/api/pages", child, a.handlePages, 409)
	if err = a.DB.Close(); err != nil {
		t.Fatal(err)
	}
	a.DB, err = initializeDatabase(a.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err = fetchPage(a, child.ID)
	if err != nil || got.Content != child.Content {
		t.Fatal("recovered page lost at second restart")
	}
}
func TestPageRecoveryMigrationAndOwnerGuards(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	db, record := dbRegressionCreate(t, a)
	recoveryCall(t, a, "DELETE", "/api/pages?id="+db.PageID+"&version=1", nil, a.handlePages, 409)
	recoveryCall(t, a, "DELETE", "/api/pages?id="+record.PageID+"&version=1", nil, a.handlePages, 409)
	p, err := fetchPage(a, record.PageID)
	if err != nil {
		t.Fatal(err)
	}
	originalContent := p.Content
	p.Content = "new record body"
	recoveryCall(t, a, "PUT", "/api/pages", p, a.handlePages, 200)
	list := recoveryCall(t, a, "GET", "/api/page-history?pageId="+p.ID, nil, a.handlePageHistory, 200)
	var revisions []pageRecoveryItem
	if err = json.Unmarshal(list.Body.Bytes(), &revisions); err != nil || len(revisions) == 0 {
		t.Fatalf("owner writes not captured: %s", list.Body.String())
	}
	rename := dbRegressionCall(t, a, "PUT", "/api/db/records", fmt.Sprintf(`{"id":%q,"version":%d,"title":"Current record title","values":{}}`, record.ID, record.Version), a.handleDBRecords)
	if rename.Code != 200 {
		t.Fatalf("rename owner: %d %s", rename.Code, rename.Body.String())
	}
	current, err := fetchPage(a, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	recoveryCall(t, a, "POST", "/api/page-history", map[string]any{"pageId": p.ID, "revisionId": revisions[0].ID, "version": current.Version}, a.handlePageHistory, 200)
	restored, err := fetchPage(a, p.ID)
	if err != nil || restored.Content != originalContent || restored.Title != "Current record title" {
		t.Fatalf("owner history restore lost current title/original body: %+v %v", restored, err)
	}
	owner, err := a.getDBRecord(record.ID)
	if err != nil || owner.Title != restored.Title {
		t.Fatalf("owner title desynchronized: %+v %v", owner, err)
	}
	if _, err = a.DB.Exec("DROP TRIGGER page_revision_capture"); err != nil {
		t.Fatal(err)
	}
	if err = verifyAppliedMigrationSchema(a.DB); err == nil {
		t.Fatal("missing recovery trigger accepted")
	}
}
func TestPageHistoryPaginationStableAndBounded(t *testing.T) {
	a := setupTestContext(t)
	defer a.DB.Close()
	p := recoveryCreate(t, a, Page{Title: "Paged", Workspace: true})
	for i := 0; i < 105; i++ {
		if _, err := a.DB.Exec("UPDATE pages SET content=?,version=version+1 WHERE id=?", fmt.Sprint(i), p.ID); err != nil {
			t.Fatal(err)
		}
	}
	res := recoveryCall(t, a, "GET", "/api/page-history?pageId="+p.ID, nil, a.handlePageHistory, 200)
	var first, second []pageRecoveryItem
	if err := json.Unmarshal(res.Body.Bytes(), &first); err != nil || len(first) != 100 {
		t.Fatal("unbounded history list")
	}
	res = recoveryCall(t, a, "GET", fmt.Sprintf("/api/page-history?pageId=%s&before=%d", p.ID, first[99].ID), nil, a.handlePageHistory, 200)
	if err := json.Unmarshal(res.Body.Bytes(), &second); err != nil || len(second) != 5 || second[0].ID >= first[99].ID {
		t.Fatal("cursor skipped/repeated history")
	}
}

// Remove the new schema only while constructing explicitly synthetic old DBs.
func removePageRecoveryTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for name := range pageRecoveryTriggers {
		if _, err := db.Exec("DROP TRIGGER " + name); err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{"DROP TABLE page_revisions", "DROP TABLE page_trash", "DELETE FROM schema_migrations WHERE version=11"} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPageRecoveryDamagedSchemaRefusesStartup(t *testing.T) {
	for _, statement := range []string{"DROP TRIGGER page_revision_capture", "DROP TABLE page_trash", "DROP INDEX idx_page_revisions_page"} {
		t.Run(statement, func(t *testing.T) {
			a := setupTestContext(t)
			p := recoveryCreate(t, a, Page{Title: "Preserve at failed startup", Workspace: true, Content: "retained"})
			if _, err := a.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if err := a.DB.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := initializeDatabase(a.RootPath)
			if err == nil {
				reopened.Close()
				t.Fatal("damaged page recovery schema accepted")
			}
			raw, err := sql.Open("sqlite", a.RootPath+"/data/planner.db")
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			var content string
			if err = raw.QueryRow("SELECT content FROM pages WHERE id=?", p.ID).Scan(&content); err != nil || content != "retained" {
				t.Fatal("failed startup changed original data")
			}
		})
	}
}
