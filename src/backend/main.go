package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const appVersion = "0.1.0"

var defaultProjects = []Project{
	{ID: "proj-galaxy", Name: "Galaxy Launch", Description: "Pre-launch work for the next release cycle.", Status: "active", Owner: "Raiden", Color: "purple"},
	{ID: "proj-ops", Name: "Ops Automation", Description: "Internal tooling and process improvements.", Status: "planning", Owner: "Ops", Color: "blue"},
	{ID: "proj-wiki", Name: "Knowledge Base", Description: "Capture product insights, docs, and research notes.", Status: "active", Owner: "Team", Color: "green"},
}

var defaultTasks = []Task{
	{ID: "task-1", ProjectID: "proj-galaxy", Title: "Finalize feature roadmap", Status: "in_progress", Priority: "high", DueDate: "2026-10-28", Assignee: "Raiden"},
	{ID: "task-2", ProjectID: "proj-galaxy", Title: "Prepare launch checklist", Status: "todo", Priority: "medium", DueDate: "2026-10-30", Assignee: "Ops"},
	{ID: "task-3", ProjectID: "proj-ops", Title: "Automate release notes", Status: "done", Priority: "low", DueDate: "2026-10-20", Assignee: "Team"},
	{ID: "task-4", ProjectID: "proj-wiki", Title: "Collect Q4 research notes", Status: "blocked", Priority: "high", DueDate: "2026-11-02", Assignee: "Raiden"},
}

type AppConfig struct {
	AppName        string `json:"appName"`
	Version        string `json:"version"`
	Port           int    `json:"port"`
	Host           string `json:"host"`
	DefaultProject string `json:"defaultProject"`
	Theme          string `json:"theme"`
	Features       struct {
		AIEnabled bool `json:"aiEnabled"`
		LocalOnly bool `json:"localOnly"`
	} `json:"features"`
}

type AIProvider struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Provider    string  `json:"provider"`
	BaseURL     string  `json:"baseURL"`
	APIKey      string  `json:"apiKey"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"maxTokens"`
	Enabled     bool    `json:"enabled"`
	Streaming   bool    `json:"streaming"`
	ToolCalling bool    `json:"toolCalling"`
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Owner       string    `json:"owner"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Task struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	DueDate     string    `json:"dueDate,omitempty"`
	Assignee    string    `json:"assignee,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AIChatRequest struct {
	Message    string `json:"message"`
	SessionID  string `json:"sessionId,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
}

type AIChatResponse struct {
	SessionID string       `json:"sessionId"`
	Response  string       `json:"response"`
	Tools     []ToolResult `json:"tools,omitempty"`
	Provider  string       `json:"provider,omitempty"`
}

type ToolResult struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Data    any    `json:"data,omitempty"`
}

type AppContext struct {
	DB       *sql.DB
	RootPath string
	Config   AppConfig
}

func main() {
	rootPath, err := resolveRootPath()
	if err != nil {
		log.Fatalf("resolve root: %v", err)
	}

	cfg, err := ensureAppConfig(rootPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := ensureProviderConfig(rootPath); err != nil {
		log.Fatalf("provider config: %v", err)
	}

	db, err := initializeDatabase(rootPath)
	if err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	defer db.Close()

	ctx := &AppContext{DB: db, RootPath: rootPath, Config: cfg}
	if err := seedDemoData(ctx); err != nil {
		log.Fatalf("seed demo data: %v", err)
	}

	http.HandleFunc("/api/health", ctx.handleHealth)
	http.HandleFunc("/api/overview", ctx.handleOverview)
	http.HandleFunc("/api/projects", ctx.handleProjects)
	http.HandleFunc("/api/tasks", ctx.handleTasks)
	http.HandleFunc("/api/settings", ctx.handleSettings)
	http.HandleFunc("/api/provider-settings", ctx.handleProviderSettings)
	http.HandleFunc("/api/ai/providers", ctx.handleProviderSettings)
	http.HandleFunc("/api/ai/test-connection", ctx.handleAIPing)
	http.HandleFunc("/api/ai/chat", ctx.handleAIChat)
	http.HandleFunc("/api/echo", ctx.handleEcho)

	staticRoot := filepath.Join(rootPath, "src", "frontend", "dist")
	if _, err := os.Stat(staticRoot); err == nil {
		fileServer := http.FileServer(http.Dir(staticRoot))
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/favicon.ico" {
				http.NotFound(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"status":"frontend_not_built","message":"Build the frontend with build.bat or npm run build before starting the app."}`)
				return
			}
			http.NotFound(w, r)
		})
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("Reverie Planner listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func resolveRootPath() (string, error) {
	for i := 0; i < len(os.Args)-1; i++ {
		if os.Args[i] == "--root" {
			return filepath.Clean(os.Args[i+1]), nil
		}
		if strings.HasPrefix(os.Args[i], "--root=") {
			return filepath.Clean(strings.TrimPrefix(os.Args[i], "--root=")), nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(cwd), nil
}

func ensureAppConfig(rootPath string) (AppConfig, error) {
	cfgPath := filepath.Join(rootPath, "config", "app.json")
	cfg := AppConfig{
		AppName: "Reverie Planner",
		Version: appVersion,
		Port:    8765,
		Host:    "127.0.0.1",
		Theme:   "dark",
	}
	cfg.Features.AIEnabled = true
	cfg.Features.LocalOnly = true

	if _, err := os.Stat(cfgPath); err == nil {
		content, err := os.ReadFile(cfgPath)
		if err != nil {
			return cfg, err
		}
		if err := json.Unmarshal(content, &cfg); err != nil {
			return cfg, err
		}
		return cfg, nil
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return cfg, err
	}
	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return cfg, err
	}
	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func ensureProviderConfig(rootPath string) error {
	cfgPath := filepath.Join(rootPath, "config", "providers.json")
	if _, err := os.Stat(cfgPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	providers := []AIProvider{{
		ID:          "default",
		Label:       "Local Ollama",
		Provider:    "openai-compatible",
		BaseURL:     "http://127.0.0.1:11434/v1",
		APIKey:      "ollama",
		Model:       "llama3.1",
		Temperature: 0.3,
		MaxTokens:   2048,
		Enabled:     true,
		Streaming:   true,
		ToolCalling: true,
	}}
	content, err := json.MarshalIndent(providers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, content, 0o644)
}

func initializeDatabase(rootPath string) (*sql.DB, error) {
	path := filepath.Join(rootPath, "data", "planner.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	migrations := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			owner TEXT,
			color TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT,
			status TEXT NOT NULL DEFAULT 'todo',
			priority TEXT NOT NULL DEFAULT 'medium',
			due_date TEXT,
			assignee TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES projects(id)
		);`,
		`CREATE TABLE IF NOT EXISTS ai_sessions (
			id TEXT PRIMARY KEY,
			provider_id TEXT,
			title TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS ai_messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(session_id) REFERENCES ai_sessions(id)
		);`,
		`CREATE TABLE IF NOT EXISTS ai_audit_log (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			action TEXT NOT NULL,
			details TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
	}
	for _, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return db, nil
}

func seedDemoData(ctx *AppContext) error {
	for _, project := range defaultProjects {
		if project.CreatedAt.IsZero() {
			project.CreatedAt = time.Now().UTC().Truncate(time.Second)
		}
		_, err := ctx.DB.Exec(
			`INSERT OR IGNORE INTO projects (id, name, description, status, owner, color, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			project.ID, project.Name, project.Description, project.Status, project.Owner, project.Color, project.CreatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return err
		}
	}
	for _, task := range defaultTasks {
		if task.CreatedAt.IsZero() {
			task.CreatedAt = time.Now().UTC().Truncate(time.Second)
		}
		_, err := ctx.DB.Exec(
			`INSERT OR IGNORE INTO tasks (id, project_id, title, description, status, priority, due_date, assignee, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			task.ID, task.ProjectID, task.Title, task.Description, task.Status, task.Priority, task.DueDate, task.Assignee, task.CreatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *AppContext) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok","version":"`+appVersion+`","database":"sqlite"}`)
}

func (a *AppContext) handleOverview(w http.ResponseWriter, r *http.Request) {
	var totalProjects, openTasks, doneTasks int
	var upcomingTasks int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&totalProjects)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&openTasks)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status = 'done'`).Scan(&doneTasks)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE due_date IS NOT NULL AND status != 'done'`).Scan(&upcomingTasks)
	payload := map[string]any{
		"projectCount": totalProjects,
		"taskCount": openTasks,
		"completedCount": doneTasks,
		"upcomingCount": upcomingTasks,
		"aiEnabled": a.Config.Features.AIEnabled,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *AppContext) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.DB.Query(`SELECT id, name, description, status, owner, color, created_at FROM projects ORDER BY created_at DESC`)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()
		var out []Project
		for rows.Next() {
			var project Project
			var created string
			if err := rows.Scan(&project.ID, &project.Name, &project.Description, &project.Status, &project.Owner, &project.Color, &created); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			project.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, project)
		}
		_ = json.NewEncoder(w).Encode(out)
	case http.MethodPost:
		var project Project
		if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid project payload")
			return
		}
		if project.ID == "" {
			project.ID = "proj-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		if strings.TrimSpace(project.Name) == "" {
			writeJSONError(w, http.StatusBadRequest, "project name is required")
			return
		}
		if project.Status == "" {
			project.Status = "active"
		}
		if project.Owner == "" {
			project.Owner = "You"
		}
		if project.Color == "" {
			project.Color = "cyan"
		}
		project.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, err := a.DB.Exec(`INSERT INTO projects (id, name, description, status, owner, color, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, project.ID, project.Name, project.Description, project.Status, project.Owner, project.Color, project.CreatedAt.Format(time.RFC3339))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(project)
	case http.MethodPut:
		var project Project
		if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid project payload")
			return
		}
		if project.ID == "" {
			writeJSONError(w, http.StatusBadRequest, "project id is required")
			return
		}
		existing, err := a.projectByID(project.ID)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		if strings.TrimSpace(project.Name) != "" {
			existing.Name = project.Name
		}
		if project.Description != "" || project.Status != "" || project.Owner != "" || project.Color != "" {
			if project.Description != "" || existing.Description == "" {
				existing.Description = project.Description
			}
			if project.Status != "" {
				existing.Status = project.Status
			}
			if project.Owner != "" {
				existing.Owner = project.Owner
			}
			if project.Color != "" {
				existing.Color = project.Color
			}
		}
		_, err = a.DB.Exec(`UPDATE projects SET name = ?, description = ?, status = ?, owner = ?, color = ? WHERE id = ?`, existing.Name, existing.Description, existing.Status, existing.Owner, existing.Color, existing.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(existing)
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSONError(w, http.StatusBadRequest, "project id is required")
				return
			}
			id = payload["id"]
		}
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "project id is required")
			return
		}
		if _, err := a.DB.Exec(`DELETE FROM tasks WHERE project_id = ?`, id); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result, err := a.DB.Exec(`DELETE FROM projects WHERE id = ?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			writeJSONError(w, http.StatusNotFound, "project not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *AppContext) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
		query := `SELECT id, project_id, title, description, status, priority, due_date, assignee, created_at FROM tasks`
		args := []any{}
		if projectID != "" {
			query += ` WHERE project_id = ?`
			args = append(args, projectID)
		}
		query += ` ORDER BY created_at DESC`
		rows, err := a.DB.Query(query, args...)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()
		var out []Task
		for rows.Next() {
			var task Task
			var created string
			if err := rows.Scan(&task.ID, &task.ProjectID, &task.Title, &task.Description, &task.Status, &task.Priority, &task.DueDate, &task.Assignee, &created); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			task.CreatedAt, _ = time.Parse(time.RFC3339, created)
			out = append(out, task)
		}
		_ = json.NewEncoder(w).Encode(out)
	case http.MethodPost:
		var task Task
		if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid task payload")
			return
		}
		if task.ID == "" {
			task.ID = "task-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		if task.ProjectID == "" || strings.TrimSpace(task.Title) == "" {
			writeJSONError(w, http.StatusBadRequest, "project id and title are required")
			return
		}
		if _, err := a.projectByID(task.ProjectID); err != nil {
			writeJSONError(w, http.StatusBadRequest, "project not found")
			return
		}
		if task.Status == "" {
			task.Status = "todo"
		}
		if task.Priority == "" {
			task.Priority = "medium"
		}
		task.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, err := a.DB.Exec(`INSERT INTO tasks (id, project_id, title, description, status, priority, due_date, assignee, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.ID, task.ProjectID, task.Title, task.Description, task.Status, task.Priority, task.DueDate, task.Assignee, task.CreatedAt.Format(time.RFC3339))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(task)
	case http.MethodPut:
		var task Task
		if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid task payload")
			return
		}
		if task.ID == "" {
			writeJSONError(w, http.StatusBadRequest, "task id is required")
			return
		}
		existing, err := a.taskByID(task.ID)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		if task.ProjectID != "" {
			existing.ProjectID = task.ProjectID
		}
		if strings.TrimSpace(task.Title) != "" {
			existing.Title = task.Title
		}
		if task.Description != "" || task.Status != "" || task.Priority != "" || task.DueDate != "" || task.Assignee != "" {
			if task.Description != "" || existing.Description == "" {
				existing.Description = task.Description
			}
			if task.Status != "" {
				existing.Status = task.Status
			}
			if task.Priority != "" {
				existing.Priority = task.Priority
			}
			if task.DueDate != "" {
				existing.DueDate = task.DueDate
			}
			if task.Assignee != "" {
				existing.Assignee = task.Assignee
			}
		}
		_, err = a.DB.Exec(`UPDATE tasks SET project_id = ?, title = ?, description = ?, status = ?, priority = ?, due_date = ?, assignee = ? WHERE id = ?`, existing.ProjectID, existing.Title, existing.Description, existing.Status, existing.Priority, existing.DueDate, existing.Assignee, existing.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(existing)
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSONError(w, http.StatusBadRequest, "task id is required")
				return
			}
			id = payload["id"]
		}
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "task id is required")
			return
		}
		result, err := a.DB.Exec(`DELETE FROM tasks WHERE id = ?`, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			writeJSONError(w, http.StatusNotFound, "task not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *AppContext) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		payload := map[string]any{"app": a.Config, "root": a.RootPath}
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	if r.Method == http.MethodPost {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid settings payload")
			return
		}
		if theme, ok := payload["theme"].(string); ok && theme != "" {
			a.Config.Theme = theme
		}
		if aiEnabled, ok := payload["aiEnabled"].(bool); ok {
			a.Config.Features.AIEnabled = aiEnabled
		}
		cfgPath := filepath.Join(a.RootPath, "config", "app.json")
		content, err := json.MarshalIndent(a.Config, "", "  ")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(a.Config)
		return
	}
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (a *AppContext) handleProviderSettings(w http.ResponseWriter, r *http.Request) {
	cfgPath := filepath.Join(a.RootPath, "config", "providers.json")
	if r.Method == http.MethodGet {
		content, err := os.ReadFile(cfgPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(content)
		return
	}
	if r.Method == http.MethodPost {
		var providers []AIProvider
		if err := json.NewDecoder(r.Body).Decode(&providers); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid provider payload")
			return
		}
		content, err := json.MarshalIndent(providers, "", "  ")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(content)
		return
	}
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (a *AppContext) handleAIPing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		ProviderID string      `json:"providerId"`
		Provider   AIProvider  `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid connection test payload")
		return
	}
	provider := req.Provider
	if provider.ID == "" && req.ProviderID != "" {
		cfg, err := readProviders(a.RootPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, item := range cfg {
			if item.ID == req.ProviderID {
				provider = item
				break
			}
		}
	}
	if provider.ID == "" {
		cfg, err := readProviders(a.RootPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(cfg) == 0 {
			writeJSONError(w, http.StatusBadRequest, "no provider configured")
			return
		}
		provider = cfg[0]
	}
	if err := testProviderConnectivity(provider); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "provider": provider.Label, "model": provider.Model})
}

func (a *AppContext) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req AIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid chat payload")
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		writeJSONError(w, http.StatusBadRequest, "message is required")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "session-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if _, err := a.DB.Exec(`INSERT OR IGNORE INTO ai_sessions (id, provider_id, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, sessionID, req.ProviderID, "AI Chat", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := a.DB.Exec(`INSERT INTO ai_messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`, "msg-"+strconv.FormatInt(time.Now().UnixNano(), 10), sessionID, "user", req.Message, time.Now().UTC().Format(time.RFC3339)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response, tools, err := a.handleAICommand(req.Message)
	if err != nil {
		response = err.Error()
	}
	if _, err := a.DB.Exec(`INSERT INTO ai_audit_log (id, session_id, action, details, created_at) VALUES (?, ?, ?, ?, ?)`, "audit-"+strconv.FormatInt(time.Now().UnixNano(), 10), sessionID, "assistant_response", fmt.Sprintf("tool_count=%d", len(tools)), time.Now().UTC().Format(time.RFC3339)); err != nil {
		log.Printf("audit log failed: %v", err)
	}
	if _, err := a.DB.Exec(`INSERT INTO ai_messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`, "msg-"+strconv.FormatInt(time.Now().UnixNano(), 10), sessionID, "assistant", response, time.Now().UTC().Format(time.RFC3339)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	payload := AIChatResponse{SessionID: sessionID, Response: response, Tools: tools}
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *AppContext) handleAICommand(message string) (string, []ToolResult, error) {
	command := strings.ToLower(strings.TrimSpace(message))
	if command == "" {
		return "", nil, errors.New("message is required")
	}
	if command == "list projects" || strings.Contains(command, "list projects") {
		projects, err := a.listProjects()
		if err != nil {
			return "", nil, err
		}
		if len(projects) == 0 {
			return "No projects found.", nil, nil
		}
		names := make([]string, 0, len(projects))
		for _, project := range projects {
			names = append(names, fmt.Sprintf("%s (%s)", project.Name, project.Status))
		}
		return "Projects: " + strings.Join(names, "; "), []ToolResult{{Name: "list_projects", Summary: "Listed projects", Data: projects}}, nil
	}
	if command == "list tasks" || strings.Contains(command, "list tasks") {
		tasks, err := a.listTasks("")
		if err != nil {
			return "", nil, err
		}
		if len(tasks) == 0 {
			return "No tasks found.", nil, nil
		}
		details := make([]string, 0, len(tasks))
		for _, task := range tasks {
			details = append(details, fmt.Sprintf("%s [%s]", task.Title, task.Status))
		}
		return "Tasks: " + strings.Join(details, "; "), []ToolResult{{Name: "list_tasks", Summary: "Listed tasks", Data: tasks}}, nil
	}
	if matches := regexp.MustCompile(`(?i)create project\s+(.+)`).FindStringSubmatch(message); len(matches) > 1 {
		projectName := strings.TrimSpace(matches[1])
		project, err := a.createProject(projectName)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Project '%s' created successfully.", project.Name), []ToolResult{{Name: "create_project", Summary: "Created project", Data: project}}, nil
	}
	if matches := regexp.MustCompile(`(?i)create task\s+(?:in\s+)?(.+?)\s*[:\-]\s*(.+)`).FindStringSubmatch(message); len(matches) > 2 {
		projectName := strings.TrimSpace(matches[1])
		taskTitle := strings.TrimSpace(matches[2])
		task, err := a.createTaskByName(projectName, taskTitle)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Task '%s' created in project '%s'.", task.Title, projectName), []ToolResult{{Name: "create_task", Summary: "Created task", Data: task}}, nil
	}
	if matches := regexp.MustCompile(`(?i)(?:complete|finish|mark done) task\s+([A-Za-z0-9-]+)`).FindStringSubmatch(message); len(matches) > 1 {
		taskID := strings.TrimSpace(matches[1])
		task, err := a.completeTask(taskID)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Task '%s' marked as done.", task.Title), []ToolResult{{Name: "complete_task", Summary: "Completed task", Data: task}}, nil
	}
	if matches := regexp.MustCompile(`(?i)search\s+(.+)`).FindStringSubmatch(message); len(matches) > 1 {
		query := strings.TrimSpace(matches[1])
		results, err := a.searchWorkspace(query)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Search results for '%s': %d match(es).", query, len(results)), []ToolResult{{Name: "search_workspace", Summary: "Searched workspace", Data: results}}, nil
	}
	return "I can help manage projects and tasks. Try 'list projects', 'create project Alpha', 'create task in Galaxy Draft roadmap', or 'complete task task-1'.", nil, nil
}

func (a *AppContext) createProject(name string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("project name is required")
	}
	project := Project{ID: "proj-" + strconv.FormatInt(time.Now().UnixNano(), 10), Name: name, Description: "Created from AI assistant", Status: "active", Owner: "You", Color: "cyan", CreatedAt: time.Now().UTC().Truncate(time.Second)}
	_, err := a.DB.Exec(`INSERT INTO projects (id, name, description, status, owner, color, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, project.ID, project.Name, project.Description, project.Status, project.Owner, project.Color, project.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (a *AppContext) createTaskByName(projectName, title string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, errors.New("task title is required")
	}
	project, err := a.findProjectByName(projectName)
	if err != nil {
		return Task{}, err
	}
	task := Task{ID: "task-" + strconv.FormatInt(time.Now().UnixNano(), 10), ProjectID: project.ID, Title: title, Description: "Created by AI assistant", Status: "todo", Priority: "medium", Assignee: "You", CreatedAt: time.Now().UTC().Truncate(time.Second)}
	_, err = a.DB.Exec(`INSERT INTO tasks (id, project_id, title, description, status, priority, due_date, assignee, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.ID, task.ProjectID, task.Title, task.Description, task.Status, task.Priority, task.DueDate, task.Assignee, task.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (a *AppContext) listProjects() ([]Project, error) {
	return a.fetchProjects()
}

func (a *AppContext) listTasks(projectID string) ([]Task, error) {
	query := `SELECT id, project_id, title, description, status, priority, due_date, assignee, created_at FROM tasks`
	args := []any{}
	if projectID != "" {
		query += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := a.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		var item Task
		var created string
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Title, &item.Description, &item.Status, &item.Priority, &item.DueDate, &item.Assignee, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, item)
	}
	return out, nil
}

func (a *AppContext) fetchProjects() ([]Project, error) {
	rows, err := a.DB.Query(`SELECT id, name, description, status, owner, color, created_at FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var item Project
		var created string
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.Owner, &item.Color, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, item)
	}
	return out, nil
}

func (a *AppContext) completeTask(taskID string) (Task, error) {
	task, err := a.taskByID(taskID)
	if err != nil {
		return Task{}, err
	}
	task.Status = "done"
	_, err = a.DB.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, task.Status, task.ID)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (a *AppContext) searchWorkspace(query string) ([]map[string]any, error) {
	q := "%" + strings.TrimSpace(query) + "%"
	results := make([]map[string]any, 0)
	rows, err := a.DB.Query(`SELECT id, name, description, status FROM projects WHERE name LIKE ? OR description LIKE ?`, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, desc, status string
		if err := rows.Scan(&id, &name, &desc, &status); err != nil {
			return nil, err
		}
		results = append(results, map[string]any{"type": "project", "id": id, "name": name, "status": status})
	}
	taskRows, err := a.DB.Query(`SELECT id, title, status, project_id FROM tasks WHERE title LIKE ? OR description LIKE ?`, q, q)
	if err != nil {
		return nil, err
	}
	defer taskRows.Close()
	for taskRows.Next() {
		var id, title, status, projectID string
		if err := taskRows.Scan(&id, &title, &status, &projectID); err != nil {
			return nil, err
		}
		results = append(results, map[string]any{"type": "task", "id": id, "title": title, "status": status, "projectId": projectID})
	}
	return results, nil
}

func (a *AppContext) findProjectByName(name string) (Project, error) {
	name = strings.TrimSpace(name)
	rows, err := a.DB.Query(`SELECT id, name, description, status, owner, color, created_at FROM projects WHERE LOWER(name) = LOWER(?) LIMIT 1`, name)
	if err != nil {
		return Project{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Project{}, errors.New("project not found")
	}
	var p Project
	var created string
	if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Owner, &p.Color, &created); err != nil {
		return Project{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return p, nil
}

func (a *AppContext) projectByID(id string) (Project, error) {
	rows, err := a.DB.Query(`SELECT id, name, description, status, owner, color, created_at FROM projects WHERE id = ? LIMIT 1`, id)
	if err != nil {
		return Project{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Project{}, errors.New("project not found")
	}
	var p Project
	var created string
	if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Owner, &p.Color, &created); err != nil {
		return Project{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return p, nil
}

func (a *AppContext) taskByID(id string) (Task, error) {
	rows, err := a.DB.Query(`SELECT id, project_id, title, description, status, priority, due_date, assignee, created_at FROM tasks WHERE id = ? LIMIT 1`, id)
	if err != nil {
		return Task{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Task{}, errors.New("task not found")
	}
	var t Task
	var created string
	if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.DueDate, &t.Assignee, &created); err != nil {
		return Task{}, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return t, nil
}

func sanitizeProviders(providers []AIProvider) []AIProvider {
	sanitized := make([]AIProvider, 0, len(providers))
	for _, provider := range providers {
		item := provider
		item.APIKey = ""
		sanitized = append(sanitized, item)
	}
	return sanitized
}

func readProviders(rootPath string) ([]AIProvider, error) {
	cfgPath := filepath.Join(rootPath, "config", "providers.json")
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, err
	}
	var providers []AIProvider
	if err := json.Unmarshal(content, &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

func testProviderConnectivity(provider AIProvider) error {
	if provider.BaseURL == "" {
		return errors.New("baseURL is required")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	endpoints := []string{strings.TrimRight(provider.BaseURL, "/") + "/models", strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"}
	for _, endpoint := range endpoints {
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		if provider.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		}
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				_ = resp.Body.Close()
				return nil
			}
			_ = resp.Body.Close()
		}
	}
	return fmt.Errorf("provider connectivity check failed for %s", provider.Label)
}

func (a *AppContext) handleEcho(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	queries := r.URL.Query()
	payload := map[string]any{"query": queries.Get("q"), "root": a.RootPath}
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func normalizeStatus(tasks []Task) map[string]int {
	counts := map[string]int{"todo": 0, "in_progress": 0, "done": 0, "blocked": 0}
	for _, task := range tasks {
		switch task.Status {
		case "todo", "in_progress", "done", "blocked":
			counts[task.Status]++
		default:
			counts["todo"]++
		}
	}
	return counts
}

func sortableProjectList(projects []Project) []Project {
	out := append([]Project{}, projects...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status == out[j].Status {
			return out[i].Name < out[j].Name
		}
		return statusWeight(out[i].Status) < statusWeight(out[j].Status)
	})
	return out
}

func statusWeight(status string) int {
	switch status {
	case "active":
		return 0
	case "planning":
		return 1
	case "paused":
		return 2
	default:
		return 3
	}
}

