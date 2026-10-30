package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxWorkspaceArchiveBytes int64 = 512 << 20
const maxWorkspaceExpandedBytes int64 = 2 << 30
const maxWorkspaceEntries = 10000

var archiveAttachmentID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,160}$`)

type archiveEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type workspaceManifest struct {
	Format        string         `json:"format"`
	Version       int            `json:"version"`
	AppVersion    string         `json:"appVersion"`
	SchemaVersion int            `json:"schemaVersion"`
	CreatedAt     string         `json:"createdAt"`
	Files         []archiveEntry `json:"files"`
}
type workspaceArchivePreview struct {
	Format        string `json:"format"`
	CreatedAt     string `json:"createdAt"`
	SchemaVersion int    `json:"schemaVersion"`
	Pages         int    `json:"pages"`
	Records       int    `json:"records"`
	Attachments   int    `json:"attachments"`
	Bytes         int64  `json:"bytes"`
	Root          string `json:"root,omitempty"`
}

// Normal mutations may run concurrently. An export holds the exclusive gate
// until its DB and attachment bytes are sealed; streaming the download is unlocked.
func (a *AppContext) workspaceMutationGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		special := r.URL.Path == "/api/workspace/archive" || r.URL.Path == "/api/workspace/restore-preview" || r.URL.Path == "/api/workspace/restore"
		lazyInitializer := r.URL.Path == "/api/workspaces" || r.URL.Path == "/api/members"
		mutation := (r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS") || lazyInitializer
		if strings.HasPrefix(r.URL.Path, "/api/") && mutation && !special {
			a.WorkspaceMu.RLock()
			defer a.WorkspaceMu.RUnlock()
			if r.Context().Err() != nil {
				writeJSONError(w, 408, "Request cancelled before mutation")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type archiveContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r archiveContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func validArchiveEntry(name string) bool {
	if name == "data/planner.db" || name == "config/app.json" {
		return true
	}
	const prefix = "data/attachments/"
	return strings.HasPrefix(name, prefix) && archiveAttachmentID.MatchString(strings.TrimPrefix(name, prefix))
}
func openArchiveInput(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(filepath.FromSlash(name))
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("archive input is not a regular file: %s", name)
	}
	f, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("archive input is not a regular file: %s", name)
	}
	return f, nil
}

func (a *AppContext) createWorkspaceArchive(ctx context.Context) (result string, err error) {
	for !a.WorkspaceMu.TryLock() {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer a.WorkspaceMu.Unlock()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = verifyArchiveCoreSchema(ctx, a.DB); err != nil {
		return "", err
	}
	if _, err = verifyPageAssetsData(ctx, a.DB); err != nil {
		return "", err
	}
	snapshot, err := createDatabaseBackup(ctx, a.DB, a.RootPath)
	if err != nil {
		return "", err
	}
	defer os.Remove(snapshot)
	directory := filepath.Join(a.RootPath, "data", "backups")
	out, err := os.CreateTemp(directory, ".workspace-*.pending")
	if err != nil {
		return "", err
	}
	pending := out.Name()
	defer os.Remove(pending)
	defer out.Close()
	if err = out.Chmod(0600); err != nil {
		return "", err
	}
	zw := zip.NewWriter(out)
	manifest := workspaceManifest{Format: "reverie-workspace", Version: 1, AppVersion: appVersion, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Files: []archiveEntry{}}
	if err = a.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&manifest.SchemaVersion); err != nil {
		return "", err
	}
	var expanded int64
	add := func(name string, reader io.Reader, expected *archiveEntry) error {
		if len(manifest.Files) >= maxWorkspaceEntries-1 {
			return errors.New("too many workspace files")
		}
		zf, e := zw.Create(name)
		if e != nil {
			return e
		}
		hash := sha256.New()
		remaining := maxWorkspaceExpandedBytes - expanded
		n, e := io.Copy(io.MultiWriter(zf, hash), io.LimitReader(archiveContextReader{ctx, reader}, remaining+1))
		if e != nil {
			return e
		}
		if n > remaining {
			return errors.New("workspace exceeds 2 GiB expanded archive limit")
		}
		expanded += n
		entry := archiveEntry{Path: name, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
		if expected != nil && (expected.Size != entry.Size || expected.SHA256 != entry.SHA256) {
			return fmt.Errorf("attachment missing or changed: %s", name)
		}
		manifest.Files = append(manifest.Files, entry)
		return nil
	}
	dbFile, err := os.Open(snapshot)
	if err != nil {
		return "", err
	}
	err = add("data/planner.db", dbFile, nil)
	dbFile.Close()
	if err != nil {
		return "", err
	}
	// Only the typed public application settings are serialized, never provider files.
	config := a.Config
	config.Version = appVersion
	config.Features.LocalOnly = true
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	if err = add("config/app.json", strings.NewReader(string(configJSON)), nil); err != nil {
		return "", err
	}
	rows, err := a.DB.QueryContext(ctx, "SELECT id,size_bytes,sha256 FROM db_files ORDER BY id")
	if err != nil {
		return "", err
	}
	files := []archiveEntry{}
	for rows.Next() {
		var id string
		var entry archiveEntry
		if err = rows.Scan(&id, &entry.Size, &entry.SHA256); err != nil {
			break
		}
		entry.Path = "data/attachments/" + id
		if !validArchiveEntry(entry.Path) {
			err = errors.New("invalid stored attachment id")
			break
		}
		files = append(files, entry)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return "", err
	}
	source, err := os.OpenRoot(a.RootPath)
	if err != nil {
		return "", err
	}
	defer source.Close()
	for _, entry := range files {
		f, e := openArchiveInput(source, entry.Path)
		if e != nil {
			return "", fmt.Errorf("attachment unavailable: %s", entry.Path)
		}
		e = add(entry.Path, f, &entry)
		f.Close()
		if e != nil {
			return "", e
		}
	}
	mf, err := zw.Create("manifest.json")
	if err != nil {
		return "", err
	}
	if err = json.NewEncoder(mf).Encode(manifest); err != nil {
		return "", err
	}
	if err = zw.Close(); err != nil {
		return "", err
	}
	if err = out.Sync(); err != nil {
		return "", err
	}
	info, err := out.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > maxWorkspaceArchiveBytes {
		return "", errors.New("archive exceeds 512 MiB limit")
	}
	if err = out.Close(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	result = filepath.Join(directory, "workspace-"+time.Now().UTC().Format("20060102T150405Z")+"-"+strings.TrimSuffix(strings.TrimPrefix(filepath.Base(pending), ".workspace-"), ".pending")+".zip")
	if err = os.Rename(pending, result); err != nil {
		return "", err
	}
	return result, nil
}

// Extract only declared regular files into a private, newly created staging root.
// No imported code, credentials, runtime files, symlinks or arbitrary paths are accepted.
func unpackWorkspaceArchive(ctx context.Context, archivePath, stage string) (preview workspaceArchivePreview, err error) {
	stat, err := os.Stat(archivePath)
	if err != nil {
		return preview, err
	}
	if stat.Size() > maxWorkspaceArchiveBytes {
		return preview, errors.New("archive exceeds 512 MiB limit")
	}
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return preview, errors.New("not a valid workspace ZIP archive")
	}
	defer zr.Close()
	if len(zr.File) < 3 || len(zr.File) > maxWorkspaceEntries {
		return preview, errors.New("invalid archive file count")
	}
	entries := map[string]*zip.File{}
	folded := map[string]bool{}
	for _, f := range zr.File {
		if f.Name != "manifest.json" && !validArchiveEntry(f.Name) {
			return preview, fmt.Errorf("unexpected archive path: %s", f.Name)
		}
		key := strings.ToLower(f.Name)
		if folded[key] || !f.Mode().IsRegular() {
			return preview, errors.New("duplicate or non-regular archive entry")
		}
		folded[key] = true
		entries[f.Name] = f
	}
	mf := entries["manifest.json"]
	if mf == nil || mf.UncompressedSize64 > 2<<20 {
		return preview, errors.New("missing or oversized archive manifest")
	}
	mfr, err := mf.Open()
	if err != nil {
		return preview, err
	}
	var manifest workspaceManifest
	decoder := json.NewDecoder(io.LimitReader(mfr, 2<<20))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&manifest)
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = errors.New("unexpected manifest content")
		}
	}
	mfr.Close()
	if err != nil || manifest.Format != "reverie-workspace" || manifest.Version != 1 {
		return preview, errors.New("unsupported or invalid workspace manifest")
	}
	if manifest.SchemaVersion < 10 || manifest.SchemaVersion > latestSupportedSchemaMigration {
		return preview, errors.New("unsupported database schema version")
	}
	if len(manifest.Files) != len(entries)-1 {
		return preview, errors.New("archive entries do not match manifest")
	}
	if _, err = time.Parse(time.RFC3339Nano, manifest.CreatedAt); err != nil {
		return preview, errors.New("invalid archive date")
	}
	seen := map[string]bool{}
	declared := map[string]archiveEntry{}
	var expanded int64
	for _, entry := range manifest.Files {
		f := entries[entry.Path]
		hashBytes, hashErr := hex.DecodeString(entry.SHA256)
		if !validArchiveEntry(entry.Path) || seen[entry.Path] || f == nil || entry.Size < 0 || hashErr != nil || len(hashBytes) != 32 || f.UncompressedSize64 != uint64(entry.Size) {
			return preview, errors.New("invalid file manifest entry")
		}
		if entry.Path == "config/app.json" && entry.Size > 64<<10 {
			return preview, errors.New("app settings exceed 64 KiB limit")
		}
		if strings.HasPrefix(entry.Path, "data/attachments/") && entry.Size > maxDatabaseFileBytes {
			return preview, errors.New("attachment exceeds supported 8 MiB limit")
		}
		if entry.Size > maxWorkspaceExpandedBytes-expanded {
			return preview, errors.New("archive exceeds 2 GiB expanded limit")
		}
		expanded += entry.Size
		seen[entry.Path] = true
		declared[entry.Path] = entry
	}
	if !seen["data/planner.db"] || !seen["config/app.json"] {
		return preview, errors.New("archive is missing database or app settings")
	}
	if err = os.MkdirAll(stage, 0700); err != nil {
		return preview, err
	}
	dest, err := os.OpenRoot(stage)
	if err != nil {
		return preview, err
	}
	defer dest.Close()
	for _, entry := range manifest.Files {
		if err = ctx.Err(); err != nil {
			return preview, err
		}
		if err = dest.MkdirAll(filepath.Dir(filepath.FromSlash(entry.Path)), 0700); err != nil {
			return preview, err
		}
		out, e := dest.OpenFile(filepath.FromSlash(entry.Path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return preview, e
		}
		input, e := entries[entry.Path].Open()
		if e != nil {
			out.Close()
			return preview, e
		}
		hash := sha256.New()
		n, e := io.Copy(io.MultiWriter(out, hash), io.LimitReader(archiveContextReader{ctx, input}, entry.Size+1))
		input.Close()
		if e == nil {
			e = out.Sync()
		}
		closeErr := out.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil || n != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
			return preview, errors.New("archive checksum or file data validation failed")
		}
	}
	cfgBytes, err := os.ReadFile(filepath.Join(stage, "config", "app.json"))
	if err != nil {
		return preview, err
	}
	var cfg AppConfig
	decoder = json.NewDecoder(strings.NewReader(string(cfgBytes)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil || !isLoopbackHost(cfg.Host) || cfg.Port < 1 || cfg.Port > 65535 || !cfg.Features.LocalOnly {
		return preview, errors.New("invalid local application settings")
	}
	var extraConfig any
	if decoder.Decode(&extraConfig) != io.EOF {
		return preview, errors.New("invalid trailing app settings")
	}
	db, err := sql.Open("sqlite", filepath.Join(stage, "data", "planner.db"))
	if err != nil {
		return preview, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return preview, errors.New("archive database failed integrity check")
	}
	if err = rejectUnsupportedSchemaMigrations(db); err != nil {
		return preview, err
	}
	if err = verifyAppliedMigrationSchema(db); err != nil {
		return preview, err
	}
	if err = verifyArchiveCoreSchema(ctx, db); err != nil {
		return preview, err
	}
	if err = verifyDatabaseForeignKeys(db); err != nil {
		return preview, err
	}
	var version int
	if err = db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&version); err != nil || version != manifest.SchemaVersion {
		return preview, errors.New("database schema does not match manifest")
	}
	rows, err := db.QueryContext(ctx, "SELECT id,size_bytes,sha256 FROM db_files")
	if err != nil {
		return preview, err
	}
	referenced := map[string]bool{}
	for rows.Next() {
		var id, hash string
		var size int64
		if err = rows.Scan(&id, &size, &hash); err != nil {
			break
		}
		name := "data/attachments/" + id
		entry, found := declared[name]
		if !found || entry.Size != size || entry.SHA256 != hash {
			err = errors.New("database attachment metadata does not match archive")
			break
		}

		referenced[name] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return preview, err
	}
	for name := range seen {
		if strings.HasPrefix(name, "data/attachments/") && !referenced[name] {
			return preview, errors.New("archive contains unreferenced attachments")
		}
	}
	pageAssets, err := verifyPageAssetsData(ctx, db)
	if err != nil {
		return preview, err
	}
	preview = workspaceArchivePreview{Format: manifest.Format, CreatedAt: manifest.CreatedAt, SchemaVersion: version, Bytes: expanded, Attachments: len(referenced) + pageAssets}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pages").Scan(&preview.Pages); err != nil {
		return preview, err
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM db_records").Scan(&preview.Records); err != nil {
		return preview, err
	}
	return preview, ctx.Err()
}

// A valid SQLite file is not necessarily a usable application database.
func verifyArchiveCoreSchema(ctx context.Context, db *sql.DB) error {
	queries := []string{
		"SELECT id,project_id,workspace,parent_id,title,icon,cover,content,created_by,created_at,updated_at,version,linked_database_id,linked_view_id FROM pages LIMIT 0",
		"SELECT key,value FROM app_state LIMIT 0",
		"SELECT id,name,description,status,owner,color,created_at FROM projects LIMIT 0",
		"SELECT id,project_id,title,description,status,priority,due_date,assignee,created_at FROM tasks LIMIT 0",
	}
	for _, query := range queries {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("archive application schema is incomplete: %w", err)
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

// --to claims a new container exclusively. Publish the validated root as its
// workspace child, avoiding cross-platform rename-overwrite races on user folders.
func restoreWorkspaceArchive(ctx context.Context, archivePath, container string) (preview workspaceArchivePreview, err error) {
	container, err = filepath.Abs(container)
	if err != nil {
		return preview, err
	}
	if err = os.Mkdir(container, 0700); err != nil {
		return preview, fmt.Errorf("restore destination must be new: %w", err)
	}
	published := false
	defer func() {
		if !published {
			os.RemoveAll(container)
		}
	}()
	stage, err := os.MkdirTemp(container, ".pending-")
	if err != nil {
		return preview, err
	}
	preview, err = unpackWorkspaceArchive(ctx, archivePath, stage)
	if err != nil {
		return preview, err
	}
	// Normal startup checks/migrations run in the disposable staged copy only.
	db, err := initializeDatabase(stage)
	if err != nil {
		return preview, err
	}
	if err = db.Close(); err != nil {
		return preview, err
	}
	if _, err = ensureAppConfig(stage); err != nil {
		return preview, err
	}
	if err = os.WriteFile(filepath.Join(stage, "config", "providers.example.json"), []byte("[]\n"), 0600); err != nil {
		return preview, err
	}
	if err = ensureProviderConfig(stage); err != nil {
		return preview, err
	}
	if err = ctx.Err(); err != nil {
		return preview, err
	}
	preview.Root = filepath.Join(container, "workspace")
	if err = os.Rename(stage, preview.Root); err != nil {
		return preview, err
	}
	published = true
	return preview, nil
}

func (a *AppContext) handleWorkspaceArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	path, err := a.createWorkspaceArchive(r.Context())
	if err != nil {
		writeJSONError(w, 500, "Workspace archive failed: "+err.Error())
		return
	}
	// The completed backup remains in data/backups even if the browser disconnects.
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	http.ServeFile(w, r, path)
}
func (a *AppContext) handleWorkspaceRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	directory := filepath.Join(a.RootPath, "data", "backups")
	if err := os.MkdirAll(directory, 0700); err != nil {
		writeJSONError(w, 500, "Restore staging is unavailable")
		return
	}
	temp, err := os.MkdirTemp(directory, ".restore-upload-")
	if err != nil {
		writeJSONError(w, 500, "Restore staging is unavailable")
		return
	}
	defer os.RemoveAll(temp)
	file, err := os.OpenFile(filepath.Join(temp, "archive.zip"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		writeJSONError(w, 500, "Archive staging failed")
		return
	}
	n, err := io.Copy(file, io.LimitReader(archiveContextReader{r.Context(), r.Body}, maxWorkspaceArchiveBytes+1))
	closeErr := file.Close()
	var limitError *http.MaxBytesError
	if n > maxWorkspaceArchiveBytes || errors.As(err, &limitError) {
		writeJSONError(w, 413, "Upload a workspace ZIP smaller than 512 MiB")
		return
	}
	if err != nil || closeErr != nil {
		if r.Context().Err() != nil {
			writeJSONError(w, 408, "Archive upload was cancelled")
		} else {
			writeJSONError(w, 500, "Archive upload could not be staged; retry or check available storage")
		}
		return
	}
	var preview workspaceArchivePreview
	if r.URL.Path == "/api/workspace/restore-preview" {
		preview, err = unpackWorkspaceArchive(r.Context(), filepath.Join(temp, "archive.zip"), filepath.Join(temp, "preview"))
	} else {
		// The generated sibling is never an arbitrary client-supplied filesystem path.
		container := filepath.Join(filepath.Dir(a.RootPath), "Reverie-Restored-"+newID("workspace"))
		preview, err = restoreWorkspaceArchive(r.Context(), filepath.Join(temp, "archive.zip"), container)
	}
	if err != nil {
		writeJSONError(w, 400, "Workspace restore refused: "+err.Error())
		return
	}
	writeJSON(w, 200, preview)
}

func runWorkspaceRestoreCLI(args []string) (bool, error) {
	requested := false
	for _, arg := range args {
		if arg == "--restore-workspace" || strings.HasPrefix(arg, "--restore-workspace=") {
			requested = true
		}
	}
	if !requested {
		return false, nil
	}
	fs := flag.NewFlagSet("restore-workspace", flag.ContinueOnError)
	archive := fs.String("restore-workspace", "", "Workspace archive ZIP")
	target := fs.String("to", "", "New destination container")
	if err := fs.Parse(args); err != nil {
		return true, err
	}
	if *archive == "" || *target == "" || fs.NArg() != 0 {
		return true, errors.New("use --restore-workspace ARCHIVE.zip --to NEW_DIRECTORY")
	}
	preview, err := restoreWorkspaceArchive(context.Background(), *archive, *target)
	if err != nil {
		return true, err
	}
	return true, json.NewEncoder(os.Stdout).Encode(preview)
}
