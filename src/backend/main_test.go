package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestContext(t *testing.T) *AppContext {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := AppConfig{AppName: "Reverie Planner", Version: appVersion, Port: 8765, Host: "127.0.0.1", Theme: "dark"}
	cfg.Features.AIEnabled = true
	cfg.Features.LocalOnly = true
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "app.json"), cfgBytes, 0o644); err != nil {
		t.Fatalf("write app config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "providers.example.json"), []byte(`[{"id":"default","label":"Local Test","provider":"openai-compatible","baseURL":"http://127.0.0.1:11434/v1","model":"test-model","enabled":false}]`), 0o600); err != nil {
		t.Fatalf("write provider example: %v", err)
	}
	if err := ensureProviderConfig(root); err != nil {
		t.Fatalf("provider config: %v", err)
	}
	db, err := initializeDatabase(root)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	return &AppContext{DB: db, RootPath: root, Config: cfg}
}

func TestProjectTaskCrudLifecycle(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	if err := seedDemoData(ctx); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	project := Project{Name: "Alpha", Description: "Test project", Status: "active", Owner: "Tester", Color: "cyan"}
	body, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(string(body)))
	res := httptest.NewRecorder()
	ctx.handleProjects(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("unexpected project create status: %d %s", res.Code, res.Body.String())
	}

	projects, err := ctx.fetchProjects()
	if err != nil {
		t.Fatalf("fetch projects: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("expected at least one project")
	}

	task := Task{ProjectID: projects[0].ID, Title: "Write tests", Status: "todo", Priority: "high", Assignee: "Tester"}
	body, err = json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(string(body)))
	res = httptest.NewRecorder()
	ctx.handleTasks(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("unexpected task create status: %d %s", res.Code, res.Body.String())
	}

	tasks, err := ctx.listTasks(projects[0].ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected a created task")
	}
	updated := tasks[0]
	updated.Status = "done"
	body, err = json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal updated task: %v", err)
	}
	req = httptest.NewRequest(http.MethodPut, "/api/tasks", strings.NewReader(string(body)))
	res = httptest.NewRecorder()
	ctx.handleTasks(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected task update status: %d %s", res.Code, res.Body.String())
	}

	if _, err := ctx.taskByID(updated.ID); err != nil {
		t.Fatalf("task lookup failed: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/tasks?id="+updated.ID, nil)
	res = httptest.NewRecorder()
	ctx.handleTasks(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("task delete status expected 204 got %d: %s", res.Code, res.Body.String())
	}
}

func TestProjectDeleteArchivesWithoutDeletingTasks(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	if err := seedDemoData(ctx); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/projects?id=proj-galaxy", nil)
	res := httptest.NewRecorder()
	ctx.handleProjects(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("archive response = %d: %s", res.Code, res.Body.String())
	}
	project, err := ctx.projectByID("proj-galaxy")
	if err != nil || project.Status != "archived" {
		t.Fatalf("project was not soft archived: status=%s err=%v", project.Status, err)
	}
	tasks, err := ctx.listTasks("proj-galaxy")
	if err != nil || len(tasks) != 2 {
		t.Fatalf("project task records were not preserved: count=%d err=%v", len(tasks), err)
	}
}

func TestProjectAndTaskAPIsValidateFieldsAndAllowClearingText(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	invalidProject := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"Bad status","status":"deleted"}`))
	invalidProjectRes := httptest.NewRecorder()
	ctx.handleProjects(invalidProjectRes, invalidProject)
	if invalidProjectRes.Code != http.StatusBadRequest {
		t.Fatalf("invalid project status accepted: %d", invalidProjectRes.Code)
	}
	project, err := ctx.createProject("Editable")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ctx.DB.Exec(`UPDATE projects SET description='old description' WHERE id=?`, project.ID); err != nil {
		t.Fatal(err)
	}
	updateProject := httptest.NewRequest(http.MethodPut, "/api/projects", strings.NewReader(`{"id":"`+project.ID+`","description":""}`))
	updateProjectRes := httptest.NewRecorder()
	ctx.handleProjects(updateProjectRes, updateProject)
	if updateProjectRes.Code != http.StatusOK {
		t.Fatalf("clear project description status=%d body=%s", updateProjectRes.Code, updateProjectRes.Body.String())
	}
	project, err = ctx.projectByID(project.ID)
	if err != nil || project.Description != "" {
		t.Fatalf("project description was not cleared: %q err=%v", project.Description, err)
	}
	invalidTask := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"projectId":"`+project.ID+`","title":"Bad task","status":"finished"}`))
	invalidTaskRes := httptest.NewRecorder()
	ctx.handleTasks(invalidTaskRes, invalidTask)
	if invalidTaskRes.Code != http.StatusBadRequest {
		t.Fatalf("invalid task status accepted: %d", invalidTaskRes.Code)
	}
}

func TestVersionedMigrationsAndConsistentSQLiteBackup(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	var migrations int
	if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrations); err != nil || migrations != 7 {
		t.Fatalf("expected seven versioned migrations; count=%d err=%v", migrations, err)
	}
	var emptyProjects int
	if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&emptyProjects); err != nil || emptyProjects != 0 {
		t.Fatalf("normal initialization must not seed demo projects: count=%d err=%v", emptyProjects, err)
	}
	project, err := ctx.createProject("Persist me")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	backupPath, err := createDatabaseBackup(context.Background(), ctx.DB, ctx.RootPath)
	if err != nil {
		t.Fatalf("create consistent backup: %v", err)
	}
	backup, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer backup.Close()
	var recovered string
	if err := backup.QueryRow(`SELECT name FROM projects WHERE id=?`, project.ID).Scan(&recovered); err != nil || recovered != project.Name {
		t.Fatalf("backup did not preserve project state: value=%q err=%v", recovered, err)
	}
}

func TestEmptyProjectAndTaskCollectionsAreJSONArrays(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	for _, testCase := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "projects", handler: ctx.handleProjects},
		{name: "tasks", handler: ctx.handleTasks},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/"+testCase.name, nil)
			res := httptest.NewRecorder()
			testCase.handler(res, req)
			if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "[]" {
				t.Fatalf("empty API collection must be [], status=%d body=%q", res.Code, res.Body.String())
			}
		})
	}
}

func TestLocalHTTPHostOriginAndSecurityHeaders(t *testing.T) {
	cfg := AppConfig{Host: "127.0.0.1", Port: 8765, DevelopmentMode: true}
	handler := localSecurityMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, testCase := range []struct {
		host   string
		origin string
		code   int
	}{
		{host: "127.0.0.1:8765", code: http.StatusNoContent},
		{host: "attacker.example", code: http.StatusForbidden},
		{host: "127.0.0.1:9999", code: http.StatusForbidden},
		{host: "127.0.0.1:8765", origin: "https://attacker.example", code: http.StatusForbidden},
		{host: "127.0.0.1:8765", origin: "http://localhost:5173", code: http.StatusNoContent},
		{host: "127.0.0.1:8765", origin: "http://localhost:5174", code: http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/health", nil)
		req.Host = testCase.host
		if testCase.origin != "" {
			req.Header.Set("Origin", testCase.origin)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != testCase.code {
			t.Errorf("host=%q origin=%q status=%d, expected %d", testCase.host, testCase.origin, res.Code, testCase.code)
		}
		if res.Header().Get("X-Content-Type-Options") != "nosniff" || res.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("security headers were not set: %v", res.Header())
		}
	}
	cfg.DevelopmentMode = false
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	res := httptest.NewRecorder()
	localSecurityMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("production middleware must not allow the dev-server origin: status=%d", res.Code)
	}
}

func TestShutdownRequiresProjectCredential(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	ctx.Shutdown = make(chan struct{}, 1)
	ctx.ShutdownToken = "test-shutdown-token"
	for _, testCase := range []struct {
		method string
		token  string
		status int
	}{
		{method: http.MethodPost, status: http.StatusUnauthorized},
		{method: http.MethodGet, token: ctx.ShutdownToken, status: http.StatusMethodNotAllowed},
		{method: http.MethodPost, token: ctx.ShutdownToken, status: http.StatusAccepted},
	} {
		req := httptest.NewRequest(testCase.method, "/api/shutdown", nil)
		if testCase.token != "" {
			req.Header.Set("Authorization", "Bearer "+testCase.token)
		}
		res := httptest.NewRecorder()
		ctx.handleShutdown(res, req)
		if res.Code != testCase.status {
			t.Errorf("shutdown status=%d expected=%d", res.Code, testCase.status)
		}
	}
	select {
	case <-ctx.Shutdown:
	default:
		t.Fatal("valid shutdown request did not notify the server")
	}
}

func TestAIChineseToolCallRequiresApprovalBeforeWriting(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if calls == 1 {
			if request["tools"] == nil {
				http.Error(w, "tools not sent", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-create","type":"function","function":{"name":"create_project","arguments":"{\"name\":\"中文项目\"}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"已创建中文项目。"}}]}`))
	}))
	defer server.Close()
	if err := writeProviders(ctx.RootPath, []AIProvider{{
		ID: "test", Label: "Mock", Provider: "openai-compatible", BaseURL: server.URL + "/v1",
		Model: "mock-model", Enabled: true, ToolCalling: true,
	}}); err != nil {
		t.Fatalf("write mock provider: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", strings.NewReader(`{"message":"请创建一个项目，名称为中文项目","providerId":"test"}`))
	res := httptest.NewRecorder()
	ctx.handleAIChat(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("AI chat status = %d: %s", res.Code, res.Body.String())
	}
	var pending AIChatResponse
	if err := json.Unmarshal(res.Body.Bytes(), &pending); err != nil {
		t.Fatalf("decode pending response: %v", err)
	}
	if len(pending.PendingApprovals) != 1 || pending.PendingApprovals[0].ToolName != "create_project" {
		t.Fatalf("expected create-project approval: %+v", pending)
	}
	pendingReq := httptest.NewRequest(http.MethodGet, "/api/ai/approvals?sessionId="+pending.SessionID, nil)
	pendingRes := httptest.NewRecorder()
	ctx.handleAIApproval(pendingRes, pendingReq)
	var restored []PendingApproval
	if pendingRes.Code != http.StatusOK || json.Unmarshal(pendingRes.Body.Bytes(), &restored) != nil || len(restored) != 1 {
		t.Fatalf("pending approval was not restored for the session: status=%d body=%s", pendingRes.Code, pendingRes.Body.String())
	}
	var count int
	if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM projects WHERE name='中文项目'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("project was written before user approval: count=%d err=%v", count, err)
	}

	approvalPayload, _ := json.Marshal(map[string]any{"id": pending.PendingApprovals[0].ID, "approve": true})
	approvalReq := httptest.NewRequest(http.MethodPost, "/api/ai/approvals", strings.NewReader(string(approvalPayload)))
	approvalRes := httptest.NewRecorder()
	ctx.handleAIApproval(approvalRes, approvalReq)
	if approvalRes.Code != http.StatusOK {
		t.Fatalf("approval status = %d: %s", approvalRes.Code, approvalRes.Body.String())
	}
	if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM projects WHERE name='中文项目'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("approved project was not committed: count=%d err=%v", count, err)
	}
	if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM ai_audit_log WHERE action='tool_write' AND details LIKE '%create_project%'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("approved write was not audited: count=%d err=%v", count, err)
	}
	if calls != 2 {
		t.Fatalf("expected model continuation after tool result, got %d calls", calls)
	}
}

func TestAIReadToolFeedbackIsPersistedAndAudited(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	if _, err := ctx.createProject("Read target"); err != nil {
		t.Fatal(err)
	}
	var calls int
	var receivedToolResult bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Messages []AgentMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-list","type":"function","function":{"name":"list_projects","arguments":"{}"}}]}}]}`))
			return
		}
		for _, message := range request.Messages {
			if message.Role == "tool" && strings.Contains(message.Content, "Read target") {
				receivedToolResult = true
			}
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"项目列表已读取。"}}]}`))
	}))
	defer server.Close()
	if err := writeProviders(ctx.RootPath, []AIProvider{{
		ID: "reader", Label: "Mock reader", Provider: "openai-compatible",
		BaseURL: server.URL + "/v1", Model: "mock-model", Enabled: true, ToolCalling: true,
	}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", strings.NewReader(`{"message":"列出项目","providerId":"reader"}`))
	res := httptest.NewRecorder()
	ctx.handleAIChat(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("AI read tool request failed: %d %s", res.Code, res.Body.String())
	}
	if calls != 2 || !receivedToolResult {
		t.Fatalf("tool result was not returned to model: calls=%d received=%v", calls, receivedToolResult)
	}
	var count int
	if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM ai_audit_log WHERE action='tool_read' AND details='list_projects'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("read tool execution was not audited: count=%d err=%v", count, err)
	}
}

func TestParseToolArgumentsRejectsTrailingData(t *testing.T) {
	if _, err := parseToolArguments("list_projects", `{} {}`); err == nil {
		t.Fatal("trailing JSON data must be rejected")
	}
}

func TestAIProjectArchiveCannotBypassHighRiskApproval(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := ctx.DB.Exec(`INSERT INTO ai_sessions (id,provider_id,title,created_at,updated_at) VALUES ('session-test','test','archive test',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	args, err := parseToolArguments("update_project", `{"id":"proj-any","status":"archived"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ctx.createPendingApproval(context.Background(), "session-test", AgentToolCall{
		ID: "call-bypass", Function: AgentFunctionCall{Name: "update_project"},
	}, args)
	if err == nil {
		t.Fatal("update_project must not bypass the separately confirmed archive tool")
	}
	archiveArgs, err := parseToolArguments("archive_project", `{"id":"proj-any"}`)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := ctx.createPendingApproval(context.Background(), "session-test", AgentToolCall{
		ID: "call-archive", Function: AgentFunctionCall{Name: "archive_project"},
	}, archiveArgs)
	if err != nil || !approval.HighRisk {
		t.Fatalf("archive tool must require high-risk confirmation: approval=%+v err=%v", approval, err)
	}
}

func TestOpenAIStreamDecodesContentAndToolCalls(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"请"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"create_project","arguments":"{\"name\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"流式项目\"}"}}]}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	var deltas strings.Builder
	message, err := decodeOpenAIStream(strings.NewReader(stream), func(event string, payload any) {
		if event == "delta" {
			deltas.WriteString(payload.(map[string]string)["content"])
		}
	})
	if err != nil {
		t.Fatalf("decode stream: %v", err)
	}
	if deltas.String() != "请" || len(message.ToolCalls) != 1 ||
		message.ToolCalls[0].Function.Name != "create_project" ||
		message.ToolCalls[0].Function.Arguments != `{"name":"流式项目"}` {
		t.Fatalf("stream content/tool call was not assembled correctly: deltas=%q calls=%+v", deltas.String(), message.ToolCalls)
	}
}

func TestLegacyProviderSecretsMigrateAndNeverReturn(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	const secret = "provider-test-secret"
	legacy := `[{"id":"default","label":"Legacy","provider":"openai-compatible","baseURL":"http://127.0.0.1:11434/v1","apiKey":"` + secret + `","model":"test-model","enabled":true}]`
	configPath := filepath.Join(ctx.RootPath, "config", "providers.json")
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	if err := ensureProviderConfig(ctx.RootPath); err != nil {
		t.Fatalf("migrate legacy config: %v", err)
	}
	disk, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	if strings.Contains(string(disk), secret) || !strings.Contains(string(disk), "apiKeyEncrypted") {
		t.Fatalf("legacy secret was not encrypted on disk: %s", disk)
	}
	providers, _, err := loadProviderFile(ctx.RootPath)
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	if len(providers) != 1 || providers[0].APIKey != secret {
		t.Fatal("migration did not preserve the provider credential")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/provider-settings", nil)
	res := httptest.NewRecorder()
	ctx.handleProviderSettings(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("provider GET returned %d: %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), secret) || strings.Contains(res.Body.String(), `"apiKey"`) {
		t.Fatalf("provider GET exposed credentials: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"apiKeyConfigured":true`) {
		t.Fatalf("provider GET omitted credential status: %s", res.Body.String())
	}
}

func TestProviderSaveEncryptsCredentialAndRetainsOnEdit(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()
	const secret = "provider-secret-value"
	payload := `[{"id":"default","label":"Local Test","provider":"openai-compatible","baseURL":"http://127.0.0.1:11434/v1","apiKey":"` + secret + `","model":"test-model","enabled":true}]`
	req := httptest.NewRequest(http.MethodPost, "/api/provider-settings", strings.NewReader(payload))
	res := httptest.NewRecorder()
	ctx.handleProviderSettings(res, req)
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), secret) {
		t.Fatalf("provider save leaked a credential or failed: %d %s", res.Code, res.Body.String())
	}
	onDisk, err := os.ReadFile(filepath.Join(ctx.RootPath, "config", "providers.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(onDisk), secret) {
		t.Fatal("provider credential is plaintext on disk")
	}

	edit := `[{"id":"default","label":"Renamed","provider":"openai-compatible","baseURL":"http://127.0.0.1:11434/v1","model":"test-model","enabled":true}]`
	req = httptest.NewRequest(http.MethodPost, "/api/provider-settings", strings.NewReader(edit))
	res = httptest.NewRecorder()
	ctx.handleProviderSettings(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("provider edit failed: %d %s", res.Code, res.Body.String())
	}
	providers, _, err := loadProviderFile(ctx.RootPath)
	if err != nil || len(providers) != 1 || providers[0].APIKey != secret {
		t.Fatalf("provider edit lost its credential: %v", err)
	}
}

func TestProviderURLRejectsInsecureAndRebindingTargets(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/v1",
		"https://10.0.0.5/v1",
		"https://169.254.169.254/v1",
		"https://user:pass@example.com/v1",
	} {
		if err := validateProviderURL(raw); err == nil {
			t.Errorf("expected provider URL %q to be rejected", raw)
		}
	}
	for _, raw := range []string{
		"http://127.0.0.1:11434/v1",
		"https://api.openai.com/v1",
	} {
		if err := validateProviderURL(raw); err != nil {
			t.Errorf("expected provider URL %q to be accepted: %v", raw, err)
		}
	}
	dial := providerDialContext(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	if _, err := dial(context.Background(), "tcp", "attacker.example:443"); err == nil {
		t.Fatal("DNS rebinding to loopback was not blocked")
	}
}

func TestProviderConnectivityRequiresModelResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer local-test-key" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Reverie Planner connection verified"}}]}`))
	}))
	defer server.Close()
	provider := AIProvider{Label: "Mock", BaseURL: server.URL + "/v1", APIKey: "local-test-key", Model: "mock"}
	if err := testProviderConnectivity(provider); err != nil {
		t.Fatalf("model response test failed: %v", err)
	}
}
