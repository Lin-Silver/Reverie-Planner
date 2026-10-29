package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const appVersion = "0.0.1"

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
	AppName         string `json:"appName"`
	Version         string `json:"version"`
	Port            int    `json:"port"`
	Host            string `json:"host"`
	DefaultProject  string `json:"defaultProject"`
	Theme           string `json:"theme"`
	DevelopmentMode bool   `json:"-"`
	Features        struct {
		AIEnabled bool `json:"aiEnabled"`
		LocalOnly bool `json:"localOnly"`
	} `json:"features"`
}

type AIProvider struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Provider    string  `json:"provider"`
	BaseURL     string  `json:"baseURL"`
	APIKey      string  `json:"-"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"maxTokens"`
	Enabled     bool    `json:"enabled"`
	Streaming   bool    `json:"streaming"`
	ToolCalling bool    `json:"toolCalling"`
}

type providerFileEntry struct {
	AIProvider
	LegacyAPIKey    string `json:"apiKey"`
	EncryptedAPIKey string `json:"apiKeyEncrypted,omitempty"`
}

type providerDTO struct {
	ID               string  `json:"id"`
	Label            string  `json:"label"`
	Provider         string  `json:"provider"`
	BaseURL          string  `json:"baseURL"`
	Model            string  `json:"model"`
	Temperature      float64 `json:"temperature"`
	MaxTokens        int     `json:"maxTokens"`
	Enabled          bool    `json:"enabled"`
	Streaming        bool    `json:"streaming"`
	ToolCalling      bool    `json:"toolCalling"`
	APIKeyConfigured bool    `json:"apiKeyConfigured"`
}

type providerInput struct {
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
	Streaming  bool   `json:"streaming,omitempty"`
}

type AIChatResponse struct {
	SessionID        string            `json:"sessionId"`
	Response         string            `json:"response"`
	Tools            []ToolResult      `json:"tools,omitempty"`
	PendingApprovals []PendingApproval `json:"pendingApprovals,omitempty"`
	Provider         string            `json:"provider,omitempty"`
}

type ToolResult struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Data    any    `json:"data,omitempty"`
}

type AppContext struct {
	DB            *sql.DB
	RootPath      string
	Config        AppConfig
	Shutdown      chan struct{}
	ShutdownToken string
}

type serviceMetadata struct {
	PID   int    `json:"pid"`
	Token string `json:"token"`
}

func main() {
	rootPath, err := resolveRootPath()
	if err != nil {
		log.Fatalf("resolve root: %v", err)
	}

	for _, arg := range os.Args[1:] {
		if arg == "--stop" {
			if err := stopPortableService(rootPath); err != nil {
				log.Fatal(err)
			}
			return
		}
	}
	cfg, err := ensureAppConfig(rootPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if !isLoopbackHost(cfg.Host) || cfg.Port < 1 || cfg.Port > 65535 {
		log.Fatal("app host must be loopback and port must be between 1 and 65535")
	}
	staticRoot := filepath.Join(rootPath, "src", "frontend", "dist")
	if _, err := os.Stat(staticRoot); errors.Is(err, os.ErrNotExist) {
		cfg.DevelopmentMode = true
	} else if err != nil {
		log.Fatalf("check frontend assets: %v", err)
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
	if demoModeRequested() {
		var seeded string
		err := db.QueryRow(`SELECT value FROM app_state WHERE key='demo_seeded'`).Scan(&seeded)
		if errors.Is(err, sql.ErrNoRows) {
			if err := seedDemoData(ctx); err != nil {
				log.Fatalf("seed demo data: %v", err)
			}
			if _, err := db.Exec(`INSERT INTO app_state (key,value) VALUES ('demo_seeded','true')`); err != nil {
				log.Fatalf("record demo mode: %v", err)
			}
		} else if err != nil {
			log.Fatalf("check demo mode: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", ctx.handleHealth)
	mux.HandleFunc("/api/workspace", ctx.handleWorkspace)
	mux.HandleFunc("/api/overview", ctx.handleOverview)
	mux.HandleFunc("/api/projects", ctx.handleProjects)
	mux.HandleFunc("/api/tasks", ctx.handleTasks)
	mux.HandleFunc("/api/settings", ctx.handleSettings)
	mux.HandleFunc("/api/provider-settings", ctx.handleProviderSettings)
	mux.HandleFunc("/api/ai/providers", ctx.handleProviderSettings)
	mux.HandleFunc("/api/ai/test-connection", ctx.handleAIPing)
	mux.HandleFunc("/api/ai/chat", ctx.handleAIChat)
	mux.HandleFunc("/api/ai/approvals", ctx.handleAIApproval)
	mux.HandleFunc("/api/ai/audit", ctx.handleAIAudit)
	mux.HandleFunc("/api/ai/sessions", ctx.handleAISessions)
	mux.HandleFunc("/api/ai/sessions/", ctx.handleAISessionMessages)
	mux.HandleFunc("/api/backup", ctx.handleBackup)
	mux.HandleFunc("/api/shutdown", ctx.handleShutdown)
	mux.HandleFunc("/api/labels", ctx.handleLabels)
	mux.HandleFunc("/api/states", ctx.handleStates)
	mux.HandleFunc("/api/cycles", ctx.handleCycles)
	mux.HandleFunc("/api/modules", ctx.handleModules)
	mux.HandleFunc("/api/pages/move", ctx.handlePageMove)
	mux.HandleFunc("/api/pages", ctx.handlePages)
	mux.HandleFunc("/api/page-library", ctx.handlePageLibrary)
	mux.HandleFunc("/api/comments", ctx.handleComments)
	mux.HandleFunc("/api/activity", ctx.handleActivity)
	mux.HandleFunc("/api/notifications", ctx.handleNotifications)
	mux.HandleFunc("/api/stickies", ctx.handleStickies)
	mux.HandleFunc("/api/drafts", ctx.handleDrafts)
	mux.HandleFunc("/api/views", ctx.handleViews)
	mux.HandleFunc("/api/intake", ctx.handleIntake)
	mux.HandleFunc("/api/search", ctx.handleSearch)
	mux.HandleFunc("/api/analytics", ctx.handleAnalytics)
	mux.HandleFunc("/api/workspaces", ctx.handleWorkspaces)
	mux.HandleFunc("/api/members", ctx.handleMembers)
	mux.HandleFunc("/api/databases", ctx.handleDatabases)
	mux.HandleFunc("/api/db/properties", ctx.handleDBProperties)
	mux.HandleFunc("/api/db/records", ctx.handleDBRecords)
	mux.HandleFunc("/api/db/records/bulk", ctx.handleDBBulkEdit)
	mux.HandleFunc("/api/db/views", ctx.handleDBViews)
	mux.HandleFunc("/api/db/linked-view", ctx.handleDBLinkedView)
	mux.HandleFunc("/api/db/trash", ctx.handleDBTrash)
	mux.HandleFunc("/api/db/files", ctx.handleDBFiles)

	if _, err := os.Stat(staticRoot); err == nil {
		fileServer := http.FileServer(http.Dir(staticRoot))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/favicon.ico" {
				http.NotFound(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"status":"frontend_not_built","message":"Build the frontend with build.bat or npm run build before starting the app."}`)
				return
			}
			http.NotFound(w, r)
		})
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen on %s: %v", addr, err)
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		_ = listener.Close()
		log.Fatalf("create shutdown credential: %v", err)
	}
	ctx.ShutdownToken = hex.EncodeToString(token)
	ctx.Shutdown = make(chan struct{}, 1)
	servicePath := filepath.Join(rootPath, "runtime", "service.json")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o700); err != nil {
		_ = listener.Close()
		log.Fatalf("create runtime directory: %v", err)
	}
	metadata, err := json.Marshal(serviceMetadata{PID: os.Getpid(), Token: ctx.ShutdownToken})
	if err != nil {
		_ = listener.Close()
		log.Fatalf("create service metadata: %v", err)
	}
	if err := os.WriteFile(servicePath, metadata, 0o600); err != nil {
		_ = listener.Close()
		log.Fatalf("write service metadata: %v", err)
	}
	server := &http.Server{Addr: addr, Handler: localSecurityMiddleware(cfg, mux)}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	log.Printf("Reverie Planner listening on %s", addr)
	select {
	case <-ctx.Shutdown:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
			_ = server.Close()
		}
	case err := <-serveResult:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server stopped: %v", err)
		}
	}
	_ = os.Remove(servicePath)
}

func resolveRootPath() (string, error) {
	for i, arg := range os.Args[1:] {
		if arg == "--root" {
			if i+2 >= len(os.Args) || strings.HasPrefix(os.Args[i+2], "--") {
				return "", errors.New("--root requires a directory")
			}
			return filepath.Abs(os.Args[i+2])
		}
		if strings.HasPrefix(arg, "--root=") {
			value := strings.TrimPrefix(arg, "--root=")
			if value == "" {
				return "", errors.New("--root requires a directory")
			}
			return filepath.Abs(value)
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
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		examplePath := filepath.Join(rootPath, "config", "providers.example.json")
		content, readErr := os.ReadFile(examplePath)
		if readErr != nil {
			return readErr
		}
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	providers, migrated, err := loadProviderFile(rootPath)
	if err != nil {
		return err
	}
	if migrated {
		return writeProviders(rootPath, providers)
	}
	return nil
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
	db.SetMaxOpenConns(1)
	// WAL allows readers to observe committed snapshots while the writer is
	// active. Verify the actual journal mode: SQLite may refuse a transition
	// (for example, when another process holds an incompatible lock).
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil || !strings.EqualFold(journalMode, "wal") {
		_ = db.Close()
		return nil, fmt.Errorf("SQLite WAL journal mode unavailable (mode=%q): %v", journalMode, err)
	}
	// FULL synchronizes WAL commits for durability across power loss. Like
	// foreign_keys, this setting is per connection, not a schema migration.
	if _, err := db.Exec(`PRAGMA synchronous = FULL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure SQLite synchronous mode: %w", err)
	}
	var synchronous int
	if err := db.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil || synchronous != 2 {
		_ = db.Close()
		return nil, fmt.Errorf("SQLite FULL synchronous mode unavailable (value=%d): %v", synchronous, err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	// PRAGMA assignments can succeed without enabling enforcement (for example
	// inside a transaction). Verify the connection state explicitly.
	var foreignKeysEnabled int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeysEnabled); err != nil || foreignKeysEnabled != 1 {
		_ = db.Close()
		return nil, fmt.Errorf("SQLite foreign-key enforcement unavailable (enabled=%d): %v", foreignKeysEnabled, err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at DATETIME NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Reject databases written by newer versions before any known migration runs.
	// Opening them with an older binary could silently damage user data.
	if err := rejectUnsupportedSchemaMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	// An applied migration must have its required schema. Reject inconsistent
	// historical databases before attempting any subsequent migration.
	if err := verifyAppliedMigrationSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	migrations := []struct {
		version    int
		statements []string
	}{
		{version: 1, statements: []string{`CREATE TABLE IF NOT EXISTS projects (
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
		);`}},
		{version: 2, statements: []string{`CREATE TABLE IF NOT EXISTS ai_sessions (
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
		);`}},
		{version: 3, statements: []string{`CREATE TABLE IF NOT EXISTS ai_pending_actions (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			tool_call_id TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			arguments TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(session_id) REFERENCES ai_sessions(id)
		);`}},
		{version: 4, statements: []string{`CREATE TABLE IF NOT EXISTS app_state (key TEXT PRIMARY KEY, value TEXT NOT NULL);`}},
		{version: 5, statements: []string{
			`CREATE TABLE IF NOT EXISTS workspaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, slug TEXT, description TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
			`CREATE TABLE IF NOT EXISTS members (id TEXT PRIMARY KEY, name TEXT NOT NULL, email TEXT, role TEXT NOT NULL DEFAULT 'member', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
			`CREATE TABLE IF NOT EXISTS labels (id TEXT PRIMARY KEY, project_id TEXT, name TEXT NOT NULL, color TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(project_id) REFERENCES projects(id));`,
			`CREATE TABLE IF NOT EXISTS states (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, color TEXT, group_name TEXT, sequence INTEGER DEFAULT 0, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(project_id) REFERENCES projects(id));`,
			`CREATE TABLE IF NOT EXISTS cycles (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, description TEXT, start_date TEXT, end_date TEXT, status TEXT NOT NULL DEFAULT 'draft', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(project_id) REFERENCES projects(id));`,
			`CREATE TABLE IF NOT EXISTS modules (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, description TEXT, status TEXT NOT NULL DEFAULT 'backlog', lead TEXT, start_date TEXT, end_date TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(project_id) REFERENCES projects(id));`,
			`CREATE TABLE IF NOT EXISTS pages (id TEXT PRIMARY KEY, project_id TEXT, workspace BOOLEAN NOT NULL DEFAULT 0, parent_id TEXT, title TEXT NOT NULL, icon TEXT, cover TEXT, content TEXT, created_by TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(project_id) REFERENCES projects(id));`,
			`CREATE TABLE IF NOT EXISTS comments (id TEXT PRIMARY KEY, entity TEXT NOT NULL, entity_id TEXT NOT NULL, author TEXT, body TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
			`CREATE TABLE IF NOT EXISTS activity (id TEXT PRIMARY KEY, entity TEXT NOT NULL, entity_id TEXT NOT NULL, action TEXT NOT NULL, actor TEXT, details TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
			`CREATE TABLE IF NOT EXISTS notifications (id TEXT PRIMARY KEY, title TEXT NOT NULL, body TEXT, type TEXT NOT NULL DEFAULT 'info', read INTEGER NOT NULL DEFAULT 0, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
			`CREATE TABLE IF NOT EXISTS stickies (id TEXT PRIMARY KEY, title TEXT, body TEXT, color TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
			`CREATE TABLE IF NOT EXISTS drafts (id TEXT PRIMARY KEY, kind TEXT NOT NULL, title TEXT, content TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
			`CREATE TABLE IF NOT EXISTS views (id TEXT PRIMARY KEY, project_id TEXT, name TEXT NOT NULL, filters TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(project_id) REFERENCES projects(id));`,
			`CREATE TABLE IF NOT EXISTS intake (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, title TEXT NOT NULL, body TEXT, status TEXT NOT NULL DEFAULT 'pending', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(project_id) REFERENCES projects(id));`,
		}},
		{version: 6, statements: []string{`ALTER TABLE pages ADD COLUMN version INTEGER NOT NULL DEFAULT 1;`}},
		{version: 7, statements: []string{
			`CREATE TABLE IF NOT EXISTS databases (
				id TEXT PRIMARY KEY,
				page_id TEXT NOT NULL,
				title TEXT NOT NULL DEFAULT 'Untitled database',
				icon TEXT,
				version INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(page_id) REFERENCES pages(id)
			);`,
			`CREATE TABLE IF NOT EXISTS db_properties (
				id TEXT PRIMARY KEY,
				database_id TEXT NOT NULL,
				name TEXT NOT NULL,
				type TEXT NOT NULL DEFAULT 'text',
				config TEXT NOT NULL DEFAULT '{}',
				position INTEGER NOT NULL DEFAULT 0,
				visible INTEGER NOT NULL DEFAULT 1,
				version INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(database_id) REFERENCES databases(id) ON DELETE CASCADE
			);`,
			`CREATE TABLE IF NOT EXISTS db_records (
				id TEXT PRIMARY KEY,
				database_id TEXT NOT NULL,
				title TEXT NOT NULL DEFAULT '',
				page_id TEXT,
				version INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(database_id) REFERENCES databases(id) ON DELETE CASCADE,
				FOREIGN KEY(page_id) REFERENCES pages(id)
			);`,
			`CREATE TABLE IF NOT EXISTS db_values (
				record_id TEXT NOT NULL,
				property_id TEXT NOT NULL,
				value TEXT NOT NULL DEFAULT 'null',
				version INTEGER NOT NULL DEFAULT 1,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY(record_id, property_id),
				FOREIGN KEY(record_id) REFERENCES db_records(id) ON DELETE CASCADE,
				FOREIGN KEY(property_id) REFERENCES db_properties(id) ON DELETE CASCADE
			);`,
			`CREATE TABLE IF NOT EXISTS db_views (
				id TEXT PRIMARY KEY,
				database_id TEXT NOT NULL,
				name TEXT NOT NULL DEFAULT 'View',
				type TEXT NOT NULL DEFAULT 'table',
				config TEXT NOT NULL DEFAULT '{}',
				position INTEGER NOT NULL DEFAULT 0,
				version INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(database_id) REFERENCES databases(id) ON DELETE CASCADE
			);`,
		}},
	}
	for _, migration := range migrations {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, migration.version).Scan(&exists); err != nil {
			_ = db.Close()
			return nil, err
		}
		if exists != 0 {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		for _, stmt := range migration.statements {
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				_ = db.Close()
				return nil, err
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version,applied_at) VALUES (?,?)`, migration.version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			_ = db.Close()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	// Database migration 8 is conditional because older releases added linked columns
	// outside schema_migrations. It must preserve both legacy and fresh databases.
	if err := migrateDatabaseLinkedColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateDatabaseTrash(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateDatabaseFiles(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Upgrade legacy task columns atomically; never ignore schema failures.
	if err := ensureTaskColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Detect pre-existing orphaned references instead of silently accepting a
	// damaged workspace. This is read-only; do not attempt destructive repairs.
	if err := verifyDatabaseForeignKeys(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// verifyDatabaseForeignKeys reports the first broken SQLite reference without
// deleting or rewriting any user records. SQLite does not automatically check
// existing rows when foreign_keys is switched on.
func verifyDatabaseForeignKeys(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("check SQLite foreign keys: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var constraint int
		if err := rows.Scan(&table, &rowID, &parent, &constraint); err != nil {
			return fmt.Errorf("read SQLite foreign-key violation: %w", err)
		}
		return fmt.Errorf("SQLite foreign-key violation in table %q (rowid=%v, referenced table=%q, constraint=%d); inspect and back up the database before repair", table, rowID, parent, constraint)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan SQLite foreign-key check: %w", err)
	}
	return nil
}

func migrateDatabaseLinkedColumns(db *sql.DB) error {
	var applied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=8").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	rows, err := db.Query("PRAGMA table_info(pages)")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, name := range []string{"linked_database_id", "linked_view_id"} {
		if !columns[name] {
			// Only known constant identifiers enter this SQL statement.
			if _, err := tx.Exec("ALTER TABLE pages ADD COLUMN " + name + " TEXT"); err != nil {
				return err
			}
		}
	}
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_db_records_database_created ON db_records(database_id, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_db_values_property ON db_values(property_id)",
		"CREATE INDEX IF NOT EXISTS idx_db_properties_order ON db_properties(database_id, position)",
		"CREATE INDEX IF NOT EXISTS idx_db_views_order ON db_views(database_id, position)",
	}
	for _, stmt := range indexes {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES (8,?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateDatabaseTrash(db *sql.DB) error {
	var exists int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=9").Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}
	tables := []string{"databases", "db_records", "db_properties", "db_views"}
	// Inspect each table before opening a write transaction: the application
	// has a one-connection SQLite pool and must not nest live row iterators.
	missing := []string{}
	for _, table := range tables {
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid, notnull, pk int
			var name, kind string
			var value sql.NullString
			if err := rows.Scan(&cid, &name, &kind, &notnull, &value, &pk); err != nil {
				rows.Close()
				return err
			}
			if name == "deleted_at" {
				found = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !found {
			missing = append(missing, table)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range missing {
		// Identifiers come only from the fixed constant list above.
		if _, err := tx.Exec("ALTER TABLE " + table + " ADD COLUMN deleted_at TEXT"); err != nil {
			return err
		}
	}
	for _, sqlText := range []string{
		"CREATE INDEX IF NOT EXISTS idx_db_records_live ON db_records(database_id, deleted_at, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_db_properties_live ON db_properties(database_id, deleted_at, position)",
		"CREATE INDEX IF NOT EXISTS idx_db_views_live ON db_views(database_id, deleted_at, position)",
	} {
		if _, err := tx.Exec(sqlText); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES (9,?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateDatabaseFiles(db *sql.DB) error {
	var applied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=10").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS db_files (
            id TEXT PRIMARY KEY,
            database_id TEXT NOT NULL,
            record_id TEXT NOT NULL,
            original_name TEXT NOT NULL,
            content_type TEXT NOT NULL,
            size_bytes INTEGER NOT NULL,
            sha256 TEXT NOT NULL,
            created_at TEXT NOT NULL,
            deleted_at TEXT,
            FOREIGN KEY(database_id) REFERENCES databases(id),
            FOREIGN KEY(record_id) REFERENCES db_records(id)
        )`,
		"CREATE INDEX IF NOT EXISTS idx_db_files_record ON db_files(record_id, deleted_at)",
		"CREATE INDEX IF NOT EXISTS idx_db_files_database ON db_files(database_id)",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES (10,?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func demoModeRequested() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--demo" {
			return true
		}
	}
	return false
}

func (a *AppContext) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path, err := createDatabaseBackup(r.Context(), a.DB, a.RootPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "database backup failed")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"file": filepath.Base(path), "status": "created"})
}

func (a *AppContext) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if a.ShutdownToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(a.ShutdownToken)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "invalid shutdown credential")
		return
	}
	select {
	case a.Shutdown <- struct{}{}:
	default:
	}
	w.WriteHeader(http.StatusAccepted)
}

// createDatabaseBackup publishes only verified, self-contained SQLite snapshots.
// No incomplete snapshot is ever exposed as a finished backup in data/backups.
func createDatabaseBackup(ctx context.Context, db *sql.DB, rootPath string) (string, error) {
	directory := filepath.Join(rootPath, "data", "backups")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	pendingDir, err := os.MkdirTemp(directory, ".pending-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(pendingDir) // remove partial SQLite files even when VACUUM fails
	pendingPath := filepath.Join(pendingDir, "snapshot.sqlite")
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, pendingPath); err != nil {
		return "", fmt.Errorf("create SQLite snapshot: %w", err)
	}
	snapshot, err := sql.Open("sqlite", pendingPath)
	if err != nil {
		return "", fmt.Errorf("open SQLite snapshot: %w", err)
	}
	snapshot.SetMaxOpenConns(1)
	var integrity string
	err = snapshot.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity)
	if err == nil && integrity != "ok" {
		err = fmt.Errorf("SQLite snapshot failed integrity_check: %s", integrity)
	}
	if err == nil {
		var violations *sql.Rows
		violations, err = snapshot.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err == nil {
			if violations.Next() {
				err = errors.New("SQLite snapshot contains foreign-key violations")
			} else {
				err = violations.Err()
			}
			if closeErr := violations.Close(); err == nil {
				err = closeErr
			}
		}
	}
	if closeErr := snapshot.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("verify SQLite snapshot: %w", err)
	}
	if err := os.Chmod(pendingPath, 0o600); err != nil {
		return "", fmt.Errorf("protect SQLite snapshot: %w", err)
	}
	snapshotFile, err := os.OpenFile(pendingPath, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	syncErr := snapshotFile.Sync()
	closeErr := snapshotFile.Close()
	if syncErr != nil {
		return "", fmt.Errorf("sync SQLite snapshot: %w", syncErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close SQLite snapshot: %w", closeErr)
	}
	// The temporary directory name is generated by the OS, preventing backups
	// made at the same nanosecond from selecting the same final filename.
	path := filepath.Join(directory, "planner-"+time.Now().UTC().Format("20060102T150405.000000000Z")+"-"+strings.TrimPrefix(filepath.Base(pendingDir), ".pending-")+".sqlite")
	if err := os.Rename(pendingPath, path); err != nil {
		return "", fmt.Errorf("publish verified SQLite snapshot: %w", err)
	}
	return path, nil
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
		"projectCount":   totalProjects,
		"taskCount":      openTasks,
		"completedCount": doneTasks,
		"upcomingCount":  upcomingTasks,
		"aiEnabled":      a.Config.Features.AIEnabled,
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
		out := make([]Project, 0)
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
		if err := rows.Err(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "projects could not be read")
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	case http.MethodPost:
		var project Project
		if _, err := decodeJSONRequest(r, &project); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid project payload")
			return
		}
		if project.ID == "" {
			project.ID = "proj-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		project.Name = strings.TrimSpace(project.Name)
		if project.Name == "" || len(project.Name) > 160 || len(project.Description) > 2000 || len(project.Owner) > 160 || len(project.Color) > 32 {
			writeJSONError(w, http.StatusBadRequest, "project fields are missing or too long")
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
		if !validProjectStatus(project.Status) {
			writeJSONError(w, http.StatusBadRequest, "invalid project status")
			return
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
		fields, err := decodeJSONRequest(r, &project)
		if err != nil {
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
		if _, ok := fields["name"]; ok {
			existing.Name = strings.TrimSpace(project.Name)
			if existing.Name == "" || len(existing.Name) > 160 {
				writeJSONError(w, http.StatusBadRequest, "project name is required and must be at most 160 characters")
				return
			}
		}
		if _, ok := fields["description"]; ok {
			existing.Description = project.Description
		}
		if _, ok := fields["status"]; ok {
			if !validProjectStatus(project.Status) {
				writeJSONError(w, http.StatusBadRequest, "invalid project status")
				return
			}
			existing.Status = project.Status
		}
		if _, ok := fields["owner"]; ok {
			existing.Owner = project.Owner
		}
		if _, ok := fields["color"]; ok {
			existing.Color = project.Color
		}
		if len(existing.Description) > 2000 || len(existing.Owner) > 160 || len(existing.Color) > 32 {
			writeJSONError(w, http.StatusBadRequest, "project fields are too long")
			return
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
			if _, err := decodeJSONRequest(r, &payload); err != nil {
				writeJSONError(w, http.StatusBadRequest, "project id is required")
				return
			}
			id = payload["id"]
		}
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "project id is required")
			return
		}
		tx, err := a.DB.BeginTx(r.Context(), nil)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result, err := tx.ExecContext(r.Context(), `UPDATE projects SET status='archived' WHERE id=?`, id)
		if err != nil {
			_ = tx.Rollback()
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows, err := result.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rows == 0 {
			_ = tx.Rollback()
			writeJSONError(w, http.StatusNotFound, "project not found")
			return
		}
		if err := tx.Commit(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "project could not be archived")
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
		out := make([]Task, 0)
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
		if err := rows.Err(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "tasks could not be read")
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	case http.MethodPost:
		var task Task
		if _, err := decodeJSONRequest(r, &task); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid task payload")
			return
		}
		if task.ID == "" {
			task.ID = "task-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		task.Title = strings.TrimSpace(task.Title)
		if task.ProjectID == "" || task.Title == "" || len(task.Title) > 240 || len(task.Description) > 4000 || len(task.Assignee) > 160 {
			writeJSONError(w, http.StatusBadRequest, "task fields are missing or too long")
			return
		}
		project, err := a.projectByID(task.ProjectID)
		if err != nil || project.Status == "archived" {
			writeJSONError(w, http.StatusBadRequest, "project not found")
			return
		}
		if task.Status == "" {
			task.Status = "todo"
		}
		if task.Priority == "" {
			task.Priority = "medium"
		}
		if !validTaskStatus(task.Status) || !validPriority(task.Priority) || (task.DueDate != "" && !validISODate(task.DueDate)) {
			writeJSONError(w, http.StatusBadRequest, "invalid task status, priority, or due date")
			return
		}
		task.CreatedAt = time.Now().UTC().Truncate(time.Second)
		_, err = a.DB.Exec(`INSERT INTO tasks (id, project_id, title, description, status, priority, due_date, assignee, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.ID, task.ProjectID, task.Title, task.Description, task.Status, task.Priority, task.DueDate, task.Assignee, task.CreatedAt.Format(time.RFC3339))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(task)
	case http.MethodPut:
		var task Task
		fields, err := decodeJSONRequest(r, &task)
		if err != nil {
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
		if _, ok := fields["projectId"]; ok {
			existing.ProjectID = task.ProjectID
		}
		if _, ok := fields["title"]; ok {
			existing.Title = strings.TrimSpace(task.Title)
			if existing.Title == "" {
				writeJSONError(w, http.StatusBadRequest, "task title is required")
				return
			}
		}
		if _, ok := fields["description"]; ok {
			existing.Description = task.Description
		}
		if _, ok := fields["status"]; ok {
			existing.Status = task.Status
		}
		if _, ok := fields["priority"]; ok {
			existing.Priority = task.Priority
		}
		if _, ok := fields["dueDate"]; ok {
			existing.DueDate = task.DueDate
		}
		if _, ok := fields["assignee"]; ok {
			existing.Assignee = task.Assignee
		}
		if len(existing.Title) > 240 || len(existing.Description) > 4000 || len(existing.Assignee) > 160 ||
			!validTaskStatus(existing.Status) || !validPriority(existing.Priority) || (existing.DueDate != "" && !validISODate(existing.DueDate)) {
			writeJSONError(w, http.StatusBadRequest, "invalid task fields")
			return
		}
		project, projectErr := a.projectByID(existing.ProjectID)
		if projectErr != nil || project.Status == "archived" {
			writeJSONError(w, http.StatusBadRequest, "active project was not found")
			return
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
			if _, err := decodeJSONRequest(r, &payload); err != nil {
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
	if r.Method == http.MethodGet {
		providers, _, err := loadProviderFile(a.RootPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "provider configuration could not be read")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toProviderDTOs(providers))
		return
	}
	if r.Method == http.MethodPost {
		var inputs []providerInput
		if err := json.NewDecoder(r.Body).Decode(&inputs); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid provider payload")
			return
		}
		current, _, err := loadProviderFile(a.RootPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "provider configuration could not be read")
			return
		}
		existingByID := make(map[string]AIProvider, len(current))
		for _, provider := range current {
			existingByID[provider.ID] = provider
		}
		providers := make([]AIProvider, 0, len(inputs))
		for _, input := range inputs {
			provider := AIProvider{
				ID: input.ID, Label: input.Label, Provider: input.Provider, BaseURL: input.BaseURL,
				APIKey: input.APIKey, Model: input.Model, Temperature: input.Temperature,
				MaxTokens: input.MaxTokens, Enabled: input.Enabled, Streaming: input.Streaming,
				ToolCalling: input.ToolCalling,
			}
			if provider.ID == "" || strings.TrimSpace(provider.Label) == "" || strings.TrimSpace(provider.Model) == "" {
				writeJSONError(w, http.StatusBadRequest, "provider id, label, and model are required")
				return
			}
			if provider.APIKey == "" {
				provider.APIKey = existingByID[provider.ID].APIKey
			}
			if err := validateProviderURL(provider.BaseURL); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid provider baseURL")
				return
			}
			providers = append(providers, provider)
		}
		if err := writeProviders(a.RootPath, providers); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "provider configuration could not be saved")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toProviderDTOs(providers))
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
		ProviderID string     `json:"providerId"`
		Provider   AIProvider `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid connection test payload")
		return
	}
	provider := req.Provider
	if provider.ID == "" && req.ProviderID != "" {
		cfg, _, err := loadProviderFile(a.RootPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "provider configuration could not be read")
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
		cfg, _, err := loadProviderFile(a.RootPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "provider configuration could not be read")
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
	out := make([]Task, 0)
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
	out := make([]Project, 0)
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

func toProviderDTOs(providers []AIProvider) []providerDTO {
	result := make([]providerDTO, 0, len(providers))
	for _, provider := range providers {
		result = append(result, providerDTO{
			ID: provider.ID, Label: provider.Label, Provider: provider.Provider, BaseURL: provider.BaseURL,
			Model: provider.Model, Temperature: provider.Temperature, MaxTokens: provider.MaxTokens,
			Enabled: provider.Enabled, Streaming: provider.Streaming, ToolCalling: provider.ToolCalling,
			APIKeyConfigured: provider.APIKey != "",
		})
	}
	return result
}

func loadProviderFile(rootPath string) ([]AIProvider, bool, error) {
	cfgPath := filepath.Join(rootPath, "config", "providers.json")
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, false, err
	}
	var stored []providerFileEntry
	if err := json.Unmarshal(content, &stored); err != nil {
		return nil, false, err
	}
	providers := make([]AIProvider, 0, len(stored))
	migrated := false
	for _, item := range stored {
		provider := item.AIProvider
		if item.EncryptedAPIKey != "" {
			provider.APIKey, err = unprotectSecret(rootPath, item.EncryptedAPIKey)
			if err != nil {
				return nil, false, fmt.Errorf("decrypt provider credential: %w", err)
			}
		} else if item.LegacyAPIKey != "" {
			provider.APIKey = item.LegacyAPIKey
			migrated = true
		}
		providers = append(providers, provider)
	}
	return providers, migrated, nil
}

func writeProviders(rootPath string, providers []AIProvider) error {
	entries := make([]providerFileEntry, 0, len(providers))
	for _, provider := range providers {
		entry := providerFileEntry{AIProvider: provider}
		if provider.APIKey != "" {
			encrypted, err := protectSecret(rootPath, provider.APIKey)
			if err != nil {
				return fmt.Errorf("encrypt provider credential: %w", err)
			}
			entry.EncryptedAPIKey = encrypted
		}
		entries = append(entries, entry)
	}
	content, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	configDir := filepath.Join(rootPath, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(configDir, "providers-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, filepath.Join(configDir, "providers.json"))
}

func validateProviderURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("invalid base URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("provider URL must use HTTP or HTTPS")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || host == "localhost.localdomain" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return nil
		}
		if parsed.Scheme != "https" || !isPublicProviderIP(ip) {
			return errors.New("remote providers must use HTTPS and a public address")
		}
		return nil
	}
	if parsed.Scheme != "https" {
		return errors.New("remote providers must use HTTPS")
	}
	return nil
}

func isPublicProviderIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}

func providerHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:       nil,
		DialContext: providerDialContext(net.DefaultResolver.LookupIPAddr),
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func providerDialContext(lookup func(context.Context, string) ([]net.IPAddr, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := lookup(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("provider host resolution failed")
		}
		hostIP := net.ParseIP(host)
		allowLoopback := strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") || (hostIP != nil && hostIP.IsLoopback())
		for _, candidate := range ips {
			if allowLoopback && candidate.IP.IsLoopback() {
				continue
			}
			if !allowLoopback && isPublicProviderIP(candidate.IP) {
				continue
			}
			return nil, errors.New("provider host resolved to a restricted network")
		}
		var lastErr error
		for _, candidate := range ips {
			if allowLoopback && !candidate.IP.IsLoopback() {
				continue
			}
			if !allowLoopback && !isPublicProviderIP(candidate.IP) {
				continue
			}
			conn, dialErr := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, errors.New("provider connection failed")
		}
		return nil, errors.New("provider address is restricted")
	}
}

func testProviderConnectivity(provider AIProvider) error {
	if err := validateProviderURL(provider.BaseURL); err != nil {
		return errors.New("provider URL is not allowed")
	}
	payload := map[string]any{
		"model": provider.Model,
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with exactly: Reverie Planner connection verified"},
		},
		"max_tokens":  32,
		"temperature": 0,
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return errors.New("provider request could not be created")
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(content)))
	if err != nil {
		return errors.New("provider request could not be created")
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	resp, err := providerHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return errors.New("provider model request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider model request returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil ||
		len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return errors.New("provider returned no model response")
	}
	return nil
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

func decodeJSONRequest(r *http.Request, target any) (map[string]json.RawMessage, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 4<<20 {
		return nil, errors.New("request body is too large")
	}
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("request payload must be an object")
	}
	if err := rejectDuplicateJSONKeys(body); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return nil, err
	}
	return fields, nil
}

func validProjectStatus(status string) bool {
	return status == "active" || status == "planning" || status == "paused" || status == "archived"
}

func validISODate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
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
