# Workspace archives and isolated restore

This feature is on dev. It extends SQLite snapshots with the attachment bytes needed to recover a complete data workspace. It is not a Notion export importer, a public sharing format, or a backup of application executables and AI credentials.

## Download and restore in Settings

1. Open **Settings → Local workspace → Take your workspace with you**.
2. Choose **Download workspace archive**. The app briefly gates its own mutations while creating a consistent SQLite snapshot and copying referenced attachment files. Reads stay available. A completed archive is retained in `data/backups` and downloaded to your browser.
3. Select the ZIP using **Workspace archive**, then choose **Preview archive**. The app validates and stages a disposable copy; preview does not alter the active workspace.
4. Review page, record and attachment counts. Choose **Restore as separate workspace** to create an adjacent `Reverie-Restored-…/workspace` directory. Existing workspaces are never replaced.
5. Keep the original application folder (or a compatible extracted portable package), stop the running app, then launch its executable with `--root` pointing at the restored directory. The restored root reuses frontend assets from that executable's package. The server prints its loopback URL.

Examples from an extracted application directory:

```sh
# Linux / macOS
./runtime/ReveriePlanner --root '/absolute/path/Reverie-Restored-…/workspace'
# Stop that restored workspace
./runtime/ReveriePlanner --root '/absolute/path/Reverie-Restored-…/workspace' --stop
```

```powershell
# Windows PowerShell
.\runtime\ReveriePlanner.exe --root 'C:\Recovered\workspace'
.\runtime\ReveriePlanner.exe --root 'C:\Recovered\workspace' --stop
```

The restored app uses the archived loopback port. Stop the original instance first, or intentionally change the restored `config/app.json` port before starting both. No live database replacement occurs. The recovered directory contains data and configuration, not another copy of the executable. Copy it together with a compatible portable package when moving to another computer.

## Offline command line restore

The runtime needs no Go, Node or Python. Choose a destination container that does **not** already exist and whose parent directory exists:

```sh
./runtime/ReveriePlanner --restore-workspace '/path/workspace.zip' --to '/path/new-recovery-container'
```

```powershell
.\runtime\ReveriePlanner.exe --restore-workspace 'C:\Backups\workspace.zip' --to 'C:\new-recovery-container'
```

The command prints JSON containing `root`, for example `/path/new-recovery-container/workspace`. Launch that root with the commands above. The destination container is claimed exclusively, data is staged privately inside it, and only the validated `workspace` child is published. Validation failure/cancellation removes the newly created container and never modifies an existing destination.

## What is preserved

- The verified SQLite snapshot: documents, nested page relationships, databases, records, typed values, views, projects/tasks, favorites/recent metadata and other application data in SQLite.
- Every referenced database attachment, including soft-deleted attachments needed for recovery. Missing, non-regular or changed attachment inputs refuse export rather than silently producing an incomplete backup.
- Typed public application settings, restricted to a loopback host and a local-only workspace.

Provider files, API keys, encryption keys, service credentials, executables, frontend bundles, logs, old backups and unreferenced filesystem files are excluded. The restored provider list starts empty; configure AI providers again if wanted. Core local features do not require them. Archives still contain private document/chat/workspace content; handle them as personal data, not sanitized samples. Copy the stopped entire original folder separately if you also need account-bound provider configuration or arbitrary extra files.

## Format and validation

ZIP format `reverie-workspace`, format version `1`, contains `manifest.json`, `data/planner.db`, `config/app.json` and `data/attachments/<opaque-id>` entries. The manifest lists size and SHA-256 for each data entry, creation time, app version and schema version. Checksums detect corruption; they do not authenticate an archive's author.

Current bounds: 512 MiB compressed, 2 GiB expanded, 10,000 total entries, 64 KiB public settings and 8 MiB per attachment. Reject unknown format/newer schema, traversal and platform-specific unsafe paths, duplicate/non-regular entries, undeclared files, checksum/size mismatch, database integrity/foreign-key errors and mismatched attachment metadata. Database validation/migration occurs only in the isolated staged copy.

Do not modify attachment files externally while the app is exporting. API mutations are gated; external filesystem edits are outside that gate. Preview uploads are temporary and cleaned up. If a browser request is cancelled after publication but before receiving its response, the completed adjacent restore can remain; inspect the generated folder instead of assuming no data was created. Interrupted downloads may still leave a completed backup in `data/backups`. Backup retention is currently manual; avoid filling the disk with repeated large exports.

## Acceptance

Synthetic Go fixtures cover attachment/trash and SQLite roundtrip, preserved source/provider config, missing/changed/symlink attachment refusal, unsafe/corrupt/newer archives, duplicate/link entries, cancellation, existing destinations and mutation gating. Browser tests download, preview, restore and start a second real service, verify attachment bytes and nested/favorite metadata, and prove edits to the recovered copy do not modify the source. The native portable script separately verifies offline CLI restore, duplicate-destination refusal, frontend asset reuse and two restored-root startup/shutdown cycles.

These tests do not establish real historical-v6/v7 sample acceptance, cross-platform manual/assistive-technology acceptance, or complete Notion import compatibility. Record SHA-matched native CI before advertising a release.
