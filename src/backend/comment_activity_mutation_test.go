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

func TestCommentActivityMutationsRejectInvalidWrites(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if strings.HasPrefix(path, "/api/comments") {
			app.handleComments(res, req)
		} else {
			app.handleActivity(res, req)
		}
		return res
	}
	commentResponse := request(http.MethodPost, "/api/comments", `{"entity":"task","entityId":"task-1","author":"Writer","body":"Keep comment"}`)
	if commentResponse.Code != http.StatusCreated {
		t.Fatalf("create comment: %d %s", commentResponse.Code, commentResponse.Body.String())
	}
	var comment Comment
	if err := json.Unmarshal(commentResponse.Body.Bytes(), &comment); err != nil || comment.ID == "" {
		t.Fatalf("decode comment: %+v err=%v", comment, err)
	}
	activityResponse := request(http.MethodPost, "/api/activity", `{"entity":"task","entityId":"task-1","action":"created","actor":"Writer","details":"Keep details"}`)
	if activityResponse.Code != http.StatusCreated {
		t.Fatalf("create activity: %d %s", activityResponse.Code, activityResponse.Body.String())
	}
	var activity Activity
	if err := json.Unmarshal(activityResponse.Body.Bytes(), &activity); err != nil || activity.ID == "" {
		t.Fatalf("decode activity: %+v err=%v", activity, err)
	}

	providerPath := filepath.Join(app.RootPath, "config", "providers.json")
	providersBefore, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		var commentCount, activityCount int
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM comments`).Scan(&commentCount); err != nil || commentCount != 1 {
			t.Fatalf("comments count=%d err=%v", commentCount, err)
		}
		if err := app.DB.QueryRow(`SELECT COUNT(*) FROM activity`).Scan(&activityCount); err != nil || activityCount != 1 {
			t.Fatalf("activity count=%d err=%v", activityCount, err)
		}
		var entity, entityID, author, body string
		if err := app.DB.QueryRow(`SELECT entity, entity_id, author, body FROM comments WHERE id=?`, comment.ID).Scan(&entity, &entityID, &author, &body); err != nil ||
			entity != "task" || entityID != "task-1" || author != "Writer" || body != "Keep comment" {
			t.Fatalf("comment changed: entity=%q id=%q author=%q body=%q err=%v", entity, entityID, author, body, err)
		}
		var action, actor, details string
		if err := app.DB.QueryRow(`SELECT entity, entity_id, action, actor, COALESCE(details, '') FROM activity WHERE id=?`, activity.ID).
			Scan(&entity, &entityID, &action, &actor, &details); err != nil ||
			entity != "task" || entityID != "task-1" || action != "created" || actor != "Writer" || details != "Keep details" {
			t.Fatalf("activity changed: entity=%q id=%q action=%q actor=%q details=%q err=%v", entity, entityID, action, actor, details, err)
		}
		providersAfter, err := os.ReadFile(providerPath)
		if err != nil || !bytes.Equal(providersBefore, providersAfter) {
			t.Fatalf("provider config changed: %v", err)
		}
	}
	invalid := []struct {
		name, method, path, body string
		status int
	}{
		{"comment malformed", http.MethodPost, "/api/comments", `{"entity":`, 400},
		{"comment duplicate", http.MethodPost, "/api/comments", `{"entity":"task","entity":"other","entityId":"task-1","body":"x"}`, 400},
		{"comment trailing", http.MethodPost, "/api/comments", `{"entity":"task","entityId":"task-1","body":"x"}{}`, 400},
		{"comment wrong type", http.MethodPost, "/api/comments", `{"entity":42,"entityId":"task-1","body":"x"}`, 400},
		{"comment array", http.MethodPost, "/api/comments", `[{"body":"x"}]`, 400},
		{"comment null", http.MethodPost, "/api/comments", `null`, 400},
		{"comment oversized", http.MethodPost, "/api/comments", fmt.Sprintf(`{"entity":"task","entityId":"task-1","body":%q}`, strings.Repeat("x", 4<<20)), 400},
		{"comment missing entity", http.MethodPost, "/api/comments", `{"entityId":"task-1","body":"x"}`, 400},
		{"comment missing entity id", http.MethodPost, "/api/comments", `{"entity":"task","body":"x"}`, 400},
		{"comment blank body", http.MethodPost, "/api/comments", `{"entity":"task","entityId":"task-1","body":"  "}`, 400},
		{"comment duplicate id", http.MethodPost, "/api/comments", fmt.Sprintf(`{"id":%q,"entity":"task","entityId":"task-1","body":"overwrite"}`, comment.ID), 500},
		{"comment delete missing id", http.MethodDelete, "/api/comments", "", 400},
		{"comment delete unknown id", http.MethodDelete, "/api/comments?id=missing", "", 404},
		{"activity malformed", http.MethodPost, "/api/activity", `{"entity":`, 400},
		{"activity duplicate", http.MethodPost, "/api/activity", `{"entity":"task","entityId":"task-1","action":"created","action":"deleted"}`, 400},
		{"activity trailing", http.MethodPost, "/api/activity", `{"entity":"task","entityId":"task-1","action":"created"}{}`, 400},
		{"activity wrong type", http.MethodPost, "/api/activity", `{"entity":"task","entityId":"task-1","action":99}`, 400},
		{"activity array", http.MethodPost, "/api/activity", `[{"action":"created"}]`, 400},
		{"activity null", http.MethodPost, "/api/activity", `null`, 400},
		{"activity oversized", http.MethodPost, "/api/activity", fmt.Sprintf(`{"entity":"task","entityId":"task-1","action":%q}`, strings.Repeat("x", 4<<20)), 400},
		{"activity missing entity", http.MethodPost, "/api/activity", `{"entityId":"task-1","action":"created"}`, 400},
		{"activity missing entity id", http.MethodPost, "/api/activity", `{"entity":"task","action":"created"}`, 400},
		{"activity missing action", http.MethodPost, "/api/activity", `{"entity":"task","entityId":"task-1"}`, 400},
		{"activity duplicate id", http.MethodPost, "/api/activity", fmt.Sprintf(`{"id":%q,"entity":"task","entityId":"task-1","action":"overwrite"}`, activity.ID), 500},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			res := request(tc.method, tc.path, tc.body)
			if res.Code != tc.status {
				t.Fatalf("expected %d got %d: %s", tc.status, res.Code, res.Body.String())
			}
			assertUnchanged(t)
		})
	}
	commentList := request(http.MethodGet, "/api/comments?entity=task&entityId=task-1", "")
	if commentList.Code != http.StatusOK || !strings.Contains(commentList.Body.String(), comment.ID) {
		t.Fatalf("comment GET failed: %d %s", commentList.Code, commentList.Body.String())
	}
	activityList := request(http.MethodGet, "/api/activity?entity=task&entityId=task-1", "")
	if activityList.Code != http.StatusOK || !strings.Contains(activityList.Body.String(), activity.ID) {
		t.Fatalf("activity GET failed: %d %s", activityList.Code, activityList.Body.String())
	}
	assertUnchanged(t)
	deleted := request(http.MethodDelete, "/api/comments?id="+comment.ID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete comment: %d %s", deleted.Code, deleted.Body.String())
	}
	var remaining int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM comments`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("comment not deleted: count=%d err=%v", remaining, err)
	}
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM activity`).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("activity changed by comment delete: count=%d err=%v", remaining, err)
	}
	providersAfter, err := os.ReadFile(providerPath)
	if err != nil || !bytes.Equal(providersBefore, providersAfter) {
		t.Fatalf("provider config changed after deletion: %v", err)
	}
}
