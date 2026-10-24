package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageMoveAtomicAndCyclePrevention(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()

	parentBody, _ := json.Marshal(Page{Title: "Parent", Workspace: true, CreatedBy: "You"})
	req := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(string(parentBody)))
	res := httptest.NewRecorder()
	ctx.handlePages(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create parent: %d %s", res.Code, res.Body.String())
	}
	var parent Page
	_ = json.Unmarshal(res.Body.Bytes(), &parent)

	childBody, _ := json.Marshal(Page{Title: "Child", Workspace: true, CreatedBy: "You"})
	req = httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(string(childBody)))
	res = httptest.NewRecorder()
	ctx.handlePages(res, req)
	var child Page
	_ = json.Unmarshal(res.Body.Bytes(), &child)

	moveBody, _ := json.Marshal(map[string]any{"id": child.ID, "parentId": parent.ID, "version": child.Version})
	req = httptest.NewRequest(http.MethodPost, "/api/pages/move", strings.NewReader(string(moveBody)))
	res = httptest.NewRecorder()
	ctx.handlePageMove(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("move: %d %s", res.Code, res.Body.String())
	}

	got, err := fetchPage(ctx, child.ID)
	if err != nil || got.ParentID != parent.ID {
		t.Fatalf("parent not updated: %v %s", err, got.ParentID)
	}

	// cycle prevention
	moveBody, _ = json.Marshal(map[string]any{"id": parent.ID, "parentId": child.ID, "version": parent.Version})
	req = httptest.NewRequest(http.MethodPost, "/api/pages/move", strings.NewReader(string(moveBody)))
	res = httptest.NewRecorder()
	ctx.handlePageMove(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected cycle rejection, got %d %s", res.Code, res.Body.String())
	}

	// version conflict
	moveBody, _ = json.Marshal(map[string]any{"id": child.ID, "parentId": "", "version": 1})
	req = httptest.NewRequest(http.MethodPost, "/api/pages/move", strings.NewReader(string(moveBody)))
	res = httptest.NewRecorder()
	ctx.handlePageMove(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d %s", res.Code, res.Body.String())
	}
}

func TestPageUpdateConflict(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()

	b, _ := json.Marshal(Page{Title: "P", Workspace: true, CreatedBy: "You"})
	req := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(string(b)))
	res := httptest.NewRecorder()
	ctx.handlePages(res, req)
	var p Page
	_ = json.Unmarshal(res.Body.Bytes(), &p)

	// stale version update should 409
	upd, _ := json.Marshal(Page{ID: p.ID, Title: "X", Workspace: true, Version: 999})
	req = httptest.NewRequest(http.MethodPut, "/api/pages", strings.NewReader(string(upd)))
	res = httptest.NewRecorder()
	ctx.handlePages(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", res.Code, res.Body.String())
	}

	// valid update succeeds
	upd, _ = json.Marshal(Page{ID: p.ID, Title: "X", Workspace: true, Version: p.Version})
	req = httptest.NewRequest(http.MethodPut, "/api/pages", strings.NewReader(string(upd)))
	res = httptest.NewRecorder()
	ctx.handlePages(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", res.Code, res.Body.String())
	}
}

func fetchPage(ctx *AppContext, id string) (Page, error) {
	req := httptest.NewRequest(http.MethodGet, "/api/pages?id="+id, nil)
	res := httptest.NewRecorder()
	ctx.handlePages(res, req)
	if res.Code != http.StatusOK {
		return Page{}, fmt.Errorf("status %d", res.Code)
	}
	var p Page
	if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
		return Page{}, err
	}
	return p, nil
}
