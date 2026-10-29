package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

// Malformed mutation bodies must be rejected before a project or task can change.
// A streaming JSON decoder alone accepts a valid first object followed by more data.
func TestProjectTaskWritesRejectTrailingJSONWithoutMutation(t *testing.T) {
 app := setupTestContext(t)
 defer app.DB.Close()

 request := func(method, path, body string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
  t.Helper()
  result := httptest.NewRecorder()
  handler(result, httptest.NewRequest(method, path, strings.NewReader(body)))
  return result
 }

 createdProject := request(http.MethodPost, "/api/projects", `{"name":"Keep project","status":"active"}`, app.handleProjects)
 if createdProject.Code != http.StatusCreated {
  t.Fatalf("project fixture create status=%d body=%s", createdProject.Code, createdProject.Body.String())
 }
 var project struct { ID string `json:"id"` }
 if err := json.Unmarshal(createdProject.Body.Bytes(), &project); err != nil || project.ID == "" {
  t.Fatalf("read project fixture id: %v", err)
 }

 createdTask := request(http.MethodPost, "/api/tasks", `{"projectId":"`+project.ID+`","title":"Keep task"}`, app.handleTasks)
 if createdTask.Code != http.StatusCreated {
  t.Fatalf("task fixture create status=%d body=%s", createdTask.Code, createdTask.Body.String())
 }
 var task struct { ID string `json:"id"` }
 if err := json.Unmarshal(createdTask.Body.Bytes(), &task); err != nil || task.ID == "" {
  t.Fatalf("read task fixture id: %v", err)
 }

 cases := []struct {
  name, method, path, body string
  handler func(http.ResponseWriter, *http.Request)
 }{
  {"project post two objects", http.MethodPost, "/api/projects", `{"name":"Unexpected"}{"name":"Other"}`, app.handleProjects},
  {"task post two objects", http.MethodPost, "/api/tasks", `{"projectId":"`+project.ID+`","title":"Unexpected"}{"title":"Other"}`, app.handleTasks},
  {"project delete two objects", http.MethodDelete, "/api/projects", `{"id":"`+project.ID+`"}{"id":"other"}`, app.handleProjects},
  {"task delete two objects", http.MethodDelete, "/api/tasks", `{"id":"`+task.ID+`"}{"id":"other"}`, app.handleTasks},
  {"project post exceeds body limit", http.MethodPost, "/api/projects", `{"name":"`+strings.Repeat("a", 4<<20)+`"}`, app.handleProjects},
 }
 for _, tc := range cases {
  t.Run(tc.name, func(t *testing.T) {
   result := request(tc.method, tc.path, tc.body, tc.handler)
   if result.Code != http.StatusBadRequest {
    t.Fatalf("wanted 400, got %d: %s", result.Code, result.Body.String())
   }
   var count int
   if err := app.DB.QueryRow("SELECT count(*) FROM projects").Scan(&count); err != nil || count != 1 {
    t.Fatalf("projects changed after rejected mutation: count=%d err=%v", count, err)
   }
   if err := app.DB.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil || count != 1 {
    t.Fatalf("tasks changed after rejected mutation: count=%d err=%v", count, err)
   }
   var status string
   if err := app.DB.QueryRow("SELECT status FROM projects WHERE id=?", project.ID).Scan(&status); err != nil || status != "active" {
    t.Fatalf("project status changed after rejected mutation: status=%q err=%v", status, err)
   }
   if err := app.DB.QueryRow("SELECT title FROM tasks WHERE id=?", task.ID).Scan(&status); err != nil || status != "Keep task" {
    t.Fatalf("task changed after rejected mutation: title=%q err=%v", status, err)
   }
  })
 }
}
