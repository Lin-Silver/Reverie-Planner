package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type AgentMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []AgentToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type AgentToolCall struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Function AgentFunctionCall `json:"function"`
}

type AgentFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type PendingApproval struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	ToolName  string          `json:"toolName"`
	Arguments json.RawMessage `json:"arguments"`
	Summary   string          `json:"summary"`
	HighRisk  bool            `json:"highRisk"`
	Status    string          `json:"status"`
	CreatedAt string          `json:"createdAt"`
}

type agentChatResponse struct {
	Choices []struct {
		Message AgentMessage `json:"message"`
		Delta   struct {
			Content   string            `json:"content"`
			ToolCalls []streamToolDelta `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

type streamToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

var agentToolDefinitions = []any{
	toolDefinition("list_projects", "List projects in the workspace", map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}),
	toolDefinition("create_project", "Create a project. Requires user approval.", objectSchema(map[string]any{
		"name": stringSchema("Project name"), "description": stringSchema("Project description"), "status": stringSchema("active, planning, or paused"), "owner": stringSchema("Project owner"), "color": stringSchema("Visual color token"),
	}, []string{"name"})),
	toolDefinition("update_project", "Update project fields. Requires user approval.", objectSchema(map[string]any{
		"id": stringSchema("Project id"), "name": stringSchema("Project name"), "description": stringSchema("Project description"), "status": stringSchema("active, planning, or paused"), "owner": stringSchema("Project owner"), "color": stringSchema("Visual color token"),
	}, []string{"id"})),
	toolDefinition("archive_project", "Archive a project. Requires explicit high-risk confirmation.", objectSchema(map[string]any{"id": stringSchema("Project id")}, []string{"id"})),
	toolDefinition("restore_project", "Restore an archived project. Requires user approval.", objectSchema(map[string]any{"id": stringSchema("Project id")}, []string{"id"})),
	toolDefinition("list_tasks", "List tasks, optionally filtered by project", objectSchema(map[string]any{"project_id": stringSchema("Project id")}, nil)),
	toolDefinition("create_task", "Create a task. Requires user approval.", objectSchema(map[string]any{
		"project_id": stringSchema("Project id"), "title": stringSchema("Task title"), "description": stringSchema("Task description"), "status": stringSchema("todo, in_progress, done, or blocked"), "priority": stringSchema("low, medium, or high"), "due_date": stringSchema("ISO date"), "assignee": stringSchema("Assignee"),
	}, []string{"project_id", "title"})),
	toolDefinition("update_task", "Update task fields. Requires user approval.", objectSchema(map[string]any{
		"id": stringSchema("Task id"), "project_id": stringSchema("Project id"), "title": stringSchema("Task title"), "description": stringSchema("Task description"), "status": stringSchema("todo, in_progress, done, or blocked"), "priority": stringSchema("low, medium, or high"), "due_date": stringSchema("ISO date"), "assignee": stringSchema("Assignee"),
	}, []string{"id"})),
	toolDefinition("complete_task", "Mark a task complete. Requires user approval.", objectSchema(map[string]any{"id": stringSchema("Task id")}, []string{"id"})),
	toolDefinition("search_workspace", "Search project and task names and descriptions", objectSchema(map[string]any{"query": stringSchema("Search text")}, []string{"query"})),
}

func toolDefinition(name, description string, parameters map[string]any) map[string]any {
	return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": description, "parameters": parameters}}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func (a *AppContext) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req AIChatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid chat payload")
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" || len(req.Message) > 32000 {
		writeJSONError(w, http.StatusBadRequest, "message is required and must be at most 32000 characters")
		return
	}
	provider, err := a.providerByID(req.ProviderID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "selected provider was not found")
		return
	}
	if !provider.Enabled {
		writeJSONError(w, http.StatusBadRequest, "selected provider is disabled")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID, err = newOpaqueID("session-")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "could not create chat session")
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := a.DB.ExecContext(r.Context(), `INSERT OR IGNORE INTO ai_sessions (id, provider_id, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, sessionID, provider.ID, truncateTitle(req.Message), now, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not create chat session")
		return
	}
	if err := a.saveAgentMessage(r.Context(), sessionID, AgentMessage{Role: "user", Content: req.Message}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not save chat message")
		return
	}
	a.serveAgentResponse(w, r, provider, sessionID, req.Streaming && provider.Streaming)
}

func (a *AppContext) serveAgentResponse(w http.ResponseWriter, r *http.Request, provider AIProvider, sessionID string, streaming bool) {
	if streaming {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
	}
	emit := func(event string, data any) {
		if !streaming {
			return
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	result, err := a.runAgent(r.Context(), provider, sessionID, emit)
	if err != nil {
		if streaming {
			emit("error", map[string]string{"error": err.Error()})
			return
		}
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if streaming {
		emit("done", result)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (a *AppContext) runAgent(ctx context.Context, provider AIProvider, sessionID string, emit func(string, any)) (AIChatResponse, error) {
	messages, err := a.loadAgentMessages(ctx, sessionID)
	if err != nil {
		return AIChatResponse{}, errors.New("chat history could not be loaded")
	}
	response := AIChatResponse{SessionID: sessionID, Provider: provider.ID}
	if !provider.ToolCalling {
		response.Response = "此 Provider 已关闭工具调用；本次仅进行纯文本对话，不会读取或修改项目数据。\n\n"
	}
	for turn := 0; turn < 8; turn++ {
		modelMessage, err := callOpenAICompatible(ctx, provider, messages, provider.ToolCalling, provider.Streaming, emit)
		if err != nil {
			if errors.Is(err, errUnsupportedToolCalling) {
				response.Response += "当前模型不支持工具调用；已降级为纯文本模式，本次不会执行项目或任务操作。\n\n"
				modelMessage, err = callOpenAICompatible(ctx, provider, messages, false, provider.Streaming, emit)
			}
			if err != nil {
				return AIChatResponse{}, err
			}
		}
		if len(modelMessage.ToolCalls) == 0 {
			response.Response += modelMessage.Content
			if err := a.saveAgentMessage(ctx, sessionID, AgentMessage{Role: "assistant", Content: modelMessage.Content}); err != nil {
				return AIChatResponse{}, errors.New("assistant response could not be saved")
			}
			return response, nil
		}
		if !provider.ToolCalling {
			response.Response += "模型请求调用工具，但此 Provider 禁用了工具调用；未执行任何操作。"
			return response, nil
		}
		modelMessage.Role = "assistant"
		if err := a.saveAgentMessage(ctx, sessionID, modelMessage); err != nil {
			return AIChatResponse{}, errors.New("tool request could not be saved")
		}
		messages = append(messages, modelMessage)
		for _, call := range modelMessage.ToolCalls {
			args, parseErr := parseToolArguments(call.Function.Name, call.Function.Arguments)
			if parseErr != nil {
				toolResult := AgentMessage{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: `{"error":"invalid tool arguments"}`}
				messages = append(messages, toolResult)
				if err := a.saveAgentMessage(ctx, sessionID, toolResult); err != nil {
					return AIChatResponse{}, errors.New("invalid tool result could not be saved")
				}
				if err := a.saveAudit(ctx, sessionID, "tool_error", call.Function.Name); err != nil {
					return AIChatResponse{}, errors.New("tool error audit could not be saved")
				}
				continue
			}
			if isWriteTool(call.Function.Name) {
				approval, err := a.createPendingApproval(ctx, sessionID, call, args)
				if err != nil {
					return AIChatResponse{}, errors.New("could not create user approval request")
				}
				response.PendingApprovals = append(response.PendingApprovals, approval)
				continue
			}
			data, toolErr := a.executeReadTool(ctx, call.Function.Name, args)
			toolMessage := toolResultMessage(call, data, toolErr)
			messages = append(messages, toolMessage)
			if err := a.saveAgentMessage(ctx, sessionID, toolMessage); err != nil {
				return AIChatResponse{}, errors.New("tool result could not be saved")
			}
			auditAction := "tool_read"
			if toolErr != nil {
				auditAction = "tool_error"
			}
			if err := a.saveAudit(ctx, sessionID, auditAction, call.Function.Name); err != nil {
				return AIChatResponse{}, errors.New("tool execution audit could not be saved")
			}
			response.Tools = append(response.Tools, ToolResult{Name: call.Function.Name, Summary: "Executed read-only tool", Data: data})
		}
		if len(response.PendingApprovals) > 0 {
			response.Response += "我已准备好所需操作，请先审核并批准后再写入数据。"
			for _, approval := range response.PendingApprovals {
				emit("approval_required", approval)
			}
			if err := a.saveAudit(ctx, sessionID, "approval_requested", fmt.Sprintf("count=%d", len(response.PendingApprovals))); err != nil {
				return AIChatResponse{}, errors.New("approval audit could not be saved")
			}
			_ = a.saveAgentMessage(ctx, sessionID, AgentMessage{Role: "assistant", Content: response.Response})
			return response, nil
		}
	}
	return AIChatResponse{}, errors.New("tool execution limit reached")
}

func callOpenAICompatible(ctx context.Context, provider AIProvider, messages []AgentMessage, toolsEnabled, streaming bool, onDelta func(string, any)) (AgentMessage, error) {
	body := map[string]any{
		"model": provider.Model,
		"messages": append([]AgentMessage{{
			Role: "system", Content: "You are Reverie Planner's project assistant. Respond in the user's language. Use registered tools for workspace data; never claim an action succeeded unless a tool result confirms it. Do not invent project or task identifiers.",
		}}, messages...),
		"temperature": provider.Temperature,
		"max_tokens":  provider.MaxTokens,
		"stream":      streaming,
	}
	if toolsEnabled {
		body["tools"] = agentToolDefinitions
		body["tool_choice"] = "auto"
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return AgentMessage{}, errors.New("provider request could not be encoded")
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return AgentMessage{}, errors.New("provider request could not be created")
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	resp, err := providerHTTPClient(3 * time.Minute).Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return AgentMessage{}, context.Canceled
		}
		return AgentMessage{}, errors.New("provider request failed; check endpoint, network, and credentials")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if toolsEnabled && (strings.Contains(strings.ToLower(string(bodyBytes)), "tool") || strings.Contains(strings.ToLower(string(bodyBytes)), "function")) {
			return AgentMessage{}, errUnsupportedToolCalling
		}
		return AgentMessage{}, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	if streaming {
		return decodeOpenAIStream(resp.Body, onDelta)
	}
	var result agentChatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil || len(result.Choices) == 0 {
		return AgentMessage{}, errors.New("provider returned an invalid chat response")
	}
	return result.Choices[0].Message, nil
}

var errUnsupportedToolCalling = errors.New("provider does not support tool calling")

func decodeOpenAIStream(reader io.Reader, onDelta func(string, any)) (AgentMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var content strings.Builder
	toolCalls := make(map[int]*AgentToolCall)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk agentChatResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return AgentMessage{}, errors.New("provider returned an invalid stream frame")
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			content.WriteString(delta.Content)
			if onDelta != nil {
				onDelta("delta", map[string]string{"content": delta.Content})
			}
		}
		for _, part := range delta.ToolCalls {
			call := toolCalls[part.Index]
			if call == nil {
				call = &AgentToolCall{Type: "function"}
				toolCalls[part.Index] = call
			}
			if part.ID != "" {
				call.ID = part.ID
			}
			if part.Type != "" {
				call.Type = part.Type
			}
			call.Function.Name += part.Function.Name
			call.Function.Arguments += part.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return AgentMessage{}, errors.New("provider stream was interrupted")
	}
	message := AgentMessage{Role: "assistant", Content: content.String()}
	if len(toolCalls) > 0 {
		indexes := make([]int, 0, len(toolCalls))
		for index := range toolCalls {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			message.ToolCalls = append(message.ToolCalls, *toolCalls[index])
		}
	}
	return message, nil
}

func (a *AppContext) providerByID(id string) (AIProvider, error) {
	providers, _, err := loadProviderFile(a.RootPath)
	if err != nil {
		return AIProvider{}, err
	}
	if id == "" && len(providers) > 0 {
		for _, provider := range providers {
			if provider.Enabled {
				return provider, nil
			}
		}
	}
	for _, provider := range providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	return AIProvider{}, errors.New("provider not found")
}

func parseToolArguments(name, raw string) (map[string]json.RawMessage, error) {
	var args map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&args); err != nil || args == nil {
		return nil, errors.New("tool arguments must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("tool arguments contain trailing data")
	}
	allowed := map[string]map[string]bool{
		"list_projects":    {"": true},
		"create_project":   {"name": true, "description": true, "status": true, "owner": true, "color": true},
		"update_project":   {"id": true, "name": true, "description": true, "status": true, "owner": true, "color": true},
		"archive_project":  {"id": true},
		"restore_project":  {"id": true},
		"list_tasks":       {"project_id": true},
		"create_task":      {"project_id": true, "title": true, "description": true, "status": true, "priority": true, "due_date": true, "assignee": true},
		"update_task":      {"id": true, "project_id": true, "title": true, "description": true, "status": true, "priority": true, "due_date": true, "assignee": true},
		"complete_task":    {"id": true},
		"search_workspace": {"query": true},
	}
	if _, ok := allowed[name]; !ok {
		return nil, errors.New("unknown tool")
	}
	for key := range args {
		if !allowed[name][key] {
			return nil, fmt.Errorf("unsupported argument %s", key)
		}
	}
	return args, nil
}

func readArg(args map[string]json.RawMessage, name string, required bool, maxLength int) (string, error) {
	raw, exists := args[name]
	if !exists {
		if required {
			return "", fmt.Errorf("%s is required", name)
		}
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	value = strings.TrimSpace(value)
	if required && value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if len(value) > maxLength {
		return "", fmt.Errorf("%s is too long", name)
	}
	return value, nil
}

func isWriteTool(name string) bool {
	switch name {
	case "create_project", "update_project", "archive_project", "restore_project", "create_task", "update_task", "complete_task":
		return true
	default:
		return false
	}
}

func toolResultMessage(call AgentToolCall, value any, err error) AgentMessage {
	var body any = map[string]any{"ok": true, "result": value}
	if err != nil {
		body = map[string]any{"ok": false, "error": err.Error()}
	}
	encoded, _ := json.Marshal(body)
	return AgentMessage{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: string(encoded)}
}

func (a *AppContext) executeReadTool(ctx context.Context, name string, args map[string]json.RawMessage) (any, error) {
	switch name {
	case "list_projects":
		return a.fetchProjects()
	case "list_tasks":
		projectID, err := readArg(args, "project_id", false, 128)
		if err != nil {
			return nil, err
		}
		return a.listTasks(projectID)
	case "search_workspace":
		query, err := readArg(args, "query", true, 512)
		if err != nil {
			return nil, err
		}
		return a.searchWorkspace(query)
	default:
		return nil, errors.New("unknown or disallowed read tool")
	}
}

func (a *AppContext) createPendingApproval(ctx context.Context, sessionID string, call AgentToolCall, args map[string]json.RawMessage) (PendingApproval, error) {
	if !isWriteTool(call.Function.Name) {
		return PendingApproval{}, errors.New("write tool is not registered")
	}
	if call.Function.Name == "update_project" {
		status, err := readArg(args, "status", false, 32)
		if err != nil {
			return PendingApproval{}, err
		}
		if status == "archived" {
			return PendingApproval{}, errors.New("use the separately confirmed archive_project tool")
		}
	}
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return PendingApproval{}, err
	}
	id, err := newOpaqueID("approval-")
	if err != nil {
		return PendingApproval{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := a.DB.ExecContext(ctx, `INSERT INTO ai_pending_actions (id, session_id, tool_call_id, tool_name, arguments, status, created_at) VALUES (?, ?, ?, ?, ?, 'pending', ?)`,
		id, sessionID, call.ID, call.Function.Name, string(encodedArgs), now); err != nil {
		return PendingApproval{}, err
	}
	return PendingApproval{
		ID: id, SessionID: sessionID, ToolName: call.Function.Name, Arguments: encodedArgs,
		Summary: approvalSummary(call.Function.Name, args), HighRisk: call.Function.Name == "archive_project",
		Status: "pending", CreatedAt: now,
	}, nil
}

func approvalSummary(name string, args map[string]json.RawMessage) string {
	field := "name"
	if name == "archive_project" || name == "restore_project" || name == "update_project" || name == "update_task" || name == "complete_task" {
		field = "id"
	}
	value, err := readArg(args, field, false, 160)
	if err != nil || value == "" {
		return name
	}
	return name + ": " + value
}

func (a *AppContext) handleAIApproval(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
		if sessionID == "" || len(sessionID) > 128 {
			writeJSONError(w, http.StatusBadRequest, "session id is required")
			return
		}
		approvals, err := a.createPendingApprovalsForSession(r.Context(), sessionID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "pending approvals could not be loaded")
			return
		}
		_ = json.NewEncoder(w).Encode(approvals)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		ID          string `json:"id"`
		Approve     bool   `json:"approve"`
		ConfirmText string `json:"confirmText"`
		Streaming   bool   `json:"streaming"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&request); err != nil || request.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "approval id is required")
		return
	}
	var approval PendingApproval
	var argsText, toolCallID string
	err := a.DB.QueryRowContext(r.Context(), `SELECT id, session_id, tool_name, arguments, status, created_at, tool_call_id FROM ai_pending_actions WHERE id = ?`, request.ID).
		Scan(&approval.ID, &approval.SessionID, &approval.ToolName, &argsText, &approval.Status, &approval.CreatedAt, &toolCallID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "approval request not found")
		return
	}
	if approval.Status != "pending" {
		writeJSONError(w, http.StatusConflict, "approval request is no longer pending")
		return
	}
	if request.Approve && approval.ToolName == "archive_project" && request.ConfirmText != "ARCHIVE" {
		writeJSONError(w, http.StatusBadRequest, "type ARCHIVE to confirm this high-impact action")
		return
	}
	args, err := parseToolArguments(approval.ToolName, argsText)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "approval arguments are invalid")
		return
	}
	var result any = map[string]string{"status": "denied", "message": "The user denied this action."}
	if request.Approve {
		result, err = a.executeWriteTool(r.Context(), approval.SessionID, request.ID, approval.ToolName, args)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		tx, err := a.DB.BeginTx(r.Context(), nil)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "approval status could not be saved")
			return
		}
		defer tx.Rollback()
		update, updateErr := tx.ExecContext(r.Context(), `UPDATE ai_pending_actions SET status = 'denied' WHERE id = ? AND status = 'pending'`, request.ID)
		if updateErr != nil {
			writeJSONError(w, http.StatusInternalServerError, "approval status could not be saved")
			return
		}
		affected, updateErr := update.RowsAffected()
		if updateErr != nil {
			writeJSONError(w, http.StatusInternalServerError, "approval status could not be saved")
			return
		}
		if affected != 1 {
			writeJSONError(w, http.StatusConflict, "approval request is no longer pending")
			return
		}
		if err := saveAuditTx(r.Context(), tx, approval.SessionID, "tool_denied", approval.ToolName); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "tool audit could not be saved")
			return
		}
		if err := tx.Commit(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "approval status could not be committed")
			return
		}
	}
	call := AgentToolCall{ID: toolCallID, Function: AgentFunctionCall{Name: approval.ToolName}}
	message := toolResultMessage(call, result, nil)
	if err := a.saveAgentMessage(r.Context(), approval.SessionID, message); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "tool result could not be saved")
		return
	}
	remaining, err := a.createPendingApprovalsForSession(r.Context(), approval.SessionID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "pending approvals could not be loaded")
		return
	}
	if len(remaining) > 0 {
		_ = json.NewEncoder(w).Encode(AIChatResponse{SessionID: approval.SessionID, Response: "请先处理其余待审批操作。", PendingApprovals: remaining})
		return
	}
	provider, err := a.providerForSession(approval.SessionID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "provider for this session was not found")
		return
	}
	a.serveAgentResponse(w, r, provider, approval.SessionID, request.Streaming && provider.Streaming)
}

func (a *AppContext) executeWriteTool(ctx context.Context, sessionID, approvalID, name string, args map[string]json.RawMessage) (any, error) {
	if !isWriteTool(name) {
		return nil, errors.New("unknown or disallowed write tool")
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.New("could not begin tool transaction")
	}
	defer tx.Rollback()
	var result any
	switch name {
	case "create_project":
		projectName, err := readArg(args, "name", true, 160)
		if err != nil {
			return nil, err
		}
		description, err := readArg(args, "description", false, 2000)
		if err != nil {
			return nil, err
		}
		status, err := readArg(args, "status", false, 32)
		if err != nil {
			return nil, err
		}
		owner, err := readArg(args, "owner", false, 160)
		if err != nil {
			return nil, err
		}
		color, err := readArg(args, "color", false, 32)
		if err != nil {
			return nil, err
		}
		if status == "" {
			status = "planning"
		}
		if status != "active" && status != "planning" && status != "paused" {
			return nil, errors.New("invalid project status")
		}
		if owner == "" {
			owner = "You"
		}
		if color == "" {
			color = "cyan"
		}
		id, err := newOpaqueID("proj-")
		if err != nil {
			return nil, err
		}
		project := Project{ID: id, Name: projectName, Description: description, Status: status, Owner: owner, Color: color, CreatedAt: time.Now().UTC()}
		_, err = tx.ExecContext(ctx, `INSERT INTO projects (id,name,description,status,owner,color,created_at) VALUES (?,?,?,?,?,?,?)`, project.ID, project.Name, project.Description, project.Status, project.Owner, project.Color, project.CreatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return nil, errors.New("project could not be created")
		}
		result = project
	case "update_project":
		id, err := readArg(args, "id", true, 128)
		if err != nil {
			return nil, err
		}
		project, err := a.projectByIDTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		for key, target := range map[string]*string{"name": &project.Name, "description": &project.Description, "status": &project.Status, "owner": &project.Owner, "color": &project.Color} {
			if _, exists := args[key]; !exists {
				continue
			}
			limit := 2000
			if key == "name" || key == "owner" {
				limit = 160
			} else if key == "color" || key == "status" {
				limit = 32
			}
			value, err := readArg(args, key, false, limit)
			if err != nil {
				return nil, err
			}
			*target = value
		}
		if project.Name == "" || (project.Status != "active" && project.Status != "planning" && project.Status != "paused") {
			return nil, errors.New("invalid project fields")
		}
		_, err = tx.ExecContext(ctx, `UPDATE projects SET name=?,description=?,status=?,owner=?,color=? WHERE id=?`, project.Name, project.Description, project.Status, project.Owner, project.Color, project.ID)
		if err != nil {
			return nil, errors.New("project could not be updated")
		}
		result = project
	case "archive_project", "restore_project":
		id, err := readArg(args, "id", true, 128)
		if err != nil {
			return nil, err
		}
		project, err := a.projectByIDTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if name == "archive_project" {
			if project.Status == "archived" {
				return nil, errors.New("project is already archived")
			}
			project.Status = "archived"
		} else {
			if project.Status != "archived" {
				return nil, errors.New("project is not archived")
			}
			project.Status = "active"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET status=? WHERE id=?`, project.Status, project.ID); err != nil {
			return nil, errors.New("project status could not be changed")
		}
		result = project
	case "create_task":
		projectID, err := readArg(args, "project_id", true, 128)
		if err != nil {
			return nil, err
		}
		project, err := a.projectByIDTx(ctx, tx, projectID)
		if err != nil || project.Status == "archived" {
			return nil, errors.New("active project was not found")
		}
		title, err := readArg(args, "title", true, 240)
		if err != nil {
			return nil, err
		}
		description, err := readArg(args, "description", false, 4000)
		if err != nil {
			return nil, err
		}
		status, err := readArg(args, "status", false, 32)
		if err != nil {
			return nil, err
		}
		priority, err := readArg(args, "priority", false, 32)
		if err != nil {
			return nil, err
		}
		assignee, err := readArg(args, "assignee", false, 160)
		if err != nil {
			return nil, err
		}
		dueDate, err := readArg(args, "due_date", false, 32)
		if err != nil {
			return nil, err
		}
		if status == "" {
			status = "todo"
		}
		if priority == "" {
			priority = "medium"
		}
		if !validTaskStatus(status) || !validPriority(priority) {
			return nil, errors.New("invalid task status or priority")
		}
		if dueDate != "" {
			if _, err := time.Parse("2006-01-02", dueDate); err != nil {
				return nil, errors.New("due_date must use YYYY-MM-DD")
			}
		}
		id, err := newOpaqueID("task-")
		if err != nil {
			return nil, err
		}
		task := Task{ID: id, ProjectID: projectID, Title: title, Description: description, Status: status, Priority: priority, DueDate: dueDate, Assignee: assignee, CreatedAt: time.Now().UTC()}
		_, err = tx.ExecContext(ctx, `INSERT INTO tasks (id,project_id,title,description,status,priority,due_date,assignee,created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			task.ID, task.ProjectID, task.Title, task.Description, task.Status, task.Priority, task.DueDate, task.Assignee, task.CreatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return nil, errors.New("task could not be created")
		}
		result = task
	case "update_task", "complete_task":
		id, err := readArg(args, "id", true, 128)
		if err != nil {
			return nil, err
		}
		task, err := a.taskByIDTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if name == "complete_task" {
			task.Status = "done"
		} else {
			for key, target := range map[string]*string{"project_id": &task.ProjectID, "title": &task.Title, "description": &task.Description, "status": &task.Status, "priority": &task.Priority, "due_date": &task.DueDate, "assignee": &task.Assignee} {
				if _, exists := args[key]; !exists {
					continue
				}
				value, err := readArg(args, key, false, 4000)
				if err != nil {
					return nil, err
				}
				*target = value
			}
		}
		if task.Title == "" || !validTaskStatus(task.Status) || !validPriority(task.Priority) {
			return nil, errors.New("invalid task fields")
		}
		targetProject, err := a.projectByIDTx(ctx, tx, task.ProjectID)
		if err != nil || targetProject.Status == "archived" {
			return nil, errors.New("target project was not found")
		}
		if task.DueDate != "" {
			if _, err := time.Parse("2006-01-02", task.DueDate); err != nil {
				return nil, errors.New("due_date must use YYYY-MM-DD")
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET project_id=?,title=?,description=?,status=?,priority=?,due_date=?,assignee=? WHERE id=?`,
			task.ProjectID, task.Title, task.Description, task.Status, task.Priority, task.DueDate, task.Assignee, task.ID)
		if err != nil {
			return nil, errors.New("task could not be updated")
		}
		result = task
	default:
		return nil, errors.New("unknown or disallowed write tool")
	}
	details, _ := json.Marshal(map[string]string{"tool": name})
	approvalUpdate, err := tx.ExecContext(ctx, `UPDATE ai_pending_actions SET status='approved' WHERE id=? AND status='pending'`, approvalID)
	if err != nil {
		return nil, errors.New("approval status could not be saved")
	}
	affected, _ := approvalUpdate.RowsAffected()
	if affected != 1 {
		return nil, errors.New("approval request is no longer pending")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ai_audit_log (id,session_id,action,details,created_at) VALUES (?,?,?,?,?)`,
		newAuditID(), sessionID, "tool_write", string(details), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, errors.New("tool audit could not be recorded")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.New("tool transaction could not be committed")
	}
	return result, nil
}

func validTaskStatus(value string) bool {
	return value == "todo" || value == "in_progress" || value == "done" || value == "blocked"
}

func validPriority(value string) bool {
	return value == "low" || value == "medium" || value == "high"
}

func (a *AppContext) projectByIDTx(ctx context.Context, tx *sql.Tx, id string) (Project, error) {
	var project Project
	var created string
	err := tx.QueryRowContext(ctx, `SELECT id,name,description,status,owner,color,created_at FROM projects WHERE id=?`, id).
		Scan(&project.ID, &project.Name, &project.Description, &project.Status, &project.Owner, &project.Color, &created)
	if err != nil {
		return Project{}, errors.New("project was not found")
	}
	project.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if project.CreatedAt.IsZero() {
		project.CreatedAt, _ = time.Parse(time.RFC3339, created)
	}
	return project, nil
}

func (a *AppContext) taskByIDTx(ctx context.Context, tx *sql.Tx, id string) (Task, error) {
	var task Task
	var created string
	err := tx.QueryRowContext(ctx, `SELECT id,project_id,title,description,status,priority,due_date,assignee,created_at FROM tasks WHERE id=?`, id).
		Scan(&task.ID, &task.ProjectID, &task.Title, &task.Description, &task.Status, &task.Priority, &task.DueDate, &task.Assignee, &created)
	if err != nil {
		return Task{}, errors.New("task was not found")
	}
	task.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if task.CreatedAt.IsZero() {
		task.CreatedAt, _ = time.Parse(time.RFC3339, created)
	}
	return task, nil
}

func (a *AppContext) saveAgentMessage(ctx context.Context, sessionID string, message AgentMessage) error {
	content, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = a.DB.ExecContext(ctx, `INSERT INTO ai_messages (id,session_id,role,content,created_at) VALUES (?,?,?,?,?)`,
		newAuditID(), sessionID, message.Role, string(content), time.Now().UTC().Format(time.RFC3339Nano))
	if err == nil {
		_, err = a.DB.ExecContext(ctx, `UPDATE ai_sessions SET updated_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), sessionID)
	}
	return err
}

func (a *AppContext) loadAgentMessages(ctx context.Context, sessionID string) ([]AgentMessage, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT role,content FROM ai_messages WHERE session_id=? ORDER BY created_at,id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]AgentMessage, 0)
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, err
		}
		var message AgentMessage
		if json.Unmarshal([]byte(content), &message) == nil && message.Role != "" {
			messages = append(messages, message)
			continue
		}
		legacyRole := role
		if legacyRole == "assistant_response" {
			legacyRole = "assistant"
		}
		messages = append(messages, AgentMessage{Role: legacyRole, Content: content})
	}
	return messages, rows.Err()
}

func (a *AppContext) saveAudit(ctx context.Context, sessionID, action, details string) error {
	_, err := a.DB.ExecContext(ctx, `INSERT INTO ai_audit_log (id,session_id,action,details,created_at) VALUES (?,?,?,?,?)`,
		newAuditID(), sessionID, action, details, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func saveAuditTx(ctx context.Context, tx *sql.Tx, sessionID, action, details string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ai_audit_log (id,session_id,action,details,created_at) VALUES (?,?,?,?,?)`,
		newAuditID(), sessionID, action, details, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (a *AppContext) providerForSession(sessionID string) (AIProvider, error) {
	var providerID string
	if err := a.DB.QueryRow(`SELECT provider_id FROM ai_sessions WHERE id=?`, sessionID).Scan(&providerID); err != nil {
		return AIProvider{}, err
	}
	return a.providerByID(providerID)
}

func (a *AppContext) handleAISessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := a.DB.QueryContext(r.Context(), `SELECT s.id,s.title,s.provider_id,s.updated_at,COUNT(m.id) FROM ai_sessions s LEFT JOIN ai_messages m ON m.session_id=s.id GROUP BY s.id ORDER BY s.updated_at DESC LIMIT 100`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "chat sessions could not be loaded")
		return
	}
	defer rows.Close()
	type session struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Provider  string `json:"providerId"`
		UpdatedAt string `json:"updatedAt"`
		Messages  int    `json:"messageCount"`
	}
	sessions := make([]session, 0)
	for rows.Next() {
		var item session
		if err := rows.Scan(&item.ID, &item.Title, &item.Provider, &item.UpdatedAt, &item.Messages); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "chat sessions could not be read")
			return
		}
		sessions = append(sessions, item)
	}
	_ = json.NewEncoder(w).Encode(sessions)
}

func (a *AppContext) handleAIAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rows, err := a.DB.QueryContext(r.Context(), `SELECT session_id,action,details,created_at FROM ai_audit_log ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "AI operation history could not be loaded")
		return
	}
	defer rows.Close()
	type auditEvent struct {
		SessionID string `json:"sessionId"`
		Action    string `json:"action"`
		Details   string `json:"details"`
		CreatedAt string `json:"createdAt"`
	}
	events := make([]auditEvent, 0)
	for rows.Next() {
		var event auditEvent
		if err := rows.Scan(&event.SessionID, &event.Action, &event.Details, &event.CreatedAt); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "AI operation history could not be read")
			return
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "AI operation history could not be read")
		return
	}
	_ = json.NewEncoder(w).Encode(events)
}

func (a *AppContext) handleAISessionMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/ai/sessions/")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		writeJSONError(w, http.StatusBadRequest, "valid session id is required")
		return
	}
	messages, err := a.loadAgentMessages(r.Context(), sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "chat session was not found")
		return
	}
	_ = json.NewEncoder(w).Encode(messages)
}

func (a *AppContext) createPendingApprovalsForSession(ctx context.Context, sessionID string) ([]PendingApproval, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT id,session_id,tool_name,arguments,status,created_at FROM ai_pending_actions WHERE session_id=? AND status='pending' ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PendingApproval, 0)
	for rows.Next() {
		var approval PendingApproval
		var raw string
		if err := rows.Scan(&approval.ID, &approval.SessionID, &approval.ToolName, &raw, &approval.Status, &approval.CreatedAt); err != nil {
			return nil, err
		}
		approval.Arguments = json.RawMessage(raw)
		approval.HighRisk = approval.ToolName == "archive_project"
		var args map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &args)
		approval.Summary = approvalSummary(approval.ToolName, args)
		result = append(result, approval)
	}
	return result, rows.Err()
}

func newOpaqueID(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random), nil
}

func newAuditID() string {
	id, err := newOpaqueID("id-")
	if err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return id
}

func truncateTitle(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 80 {
		return value[:80]
	}
	return value
}
