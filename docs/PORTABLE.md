# V0.0.1 portable workspace

Extract a package for your operating system and CPU to a writable folder. Keep the entire folder together. Windows: start.bat / stop.bat. macOS and Linux: ./start.sh / ./stop.sh. macOS also includes start.command and stop.command for opening in Terminal. Open the loopback address printed in the terminal. Unix launchers keep the service attached to the terminal. Runtime packages need no Go, Node or Python.

## Your data

- data/planner.db: SQLite documents, records, projects and history. Inspect a **copy** using a SQLite viewer.
- data/attachments: uploaded database attachments.
- data/backups: consistent SQLite snapshots created by the backup action.
- config/providers.json: encrypted provider credentials; keep private.
- data/provider.key: non-Windows encryption key; keep with encrypted provider config and never share it.
- runtime/service.json: temporary ownership and shutdown credential. Do not edit or share while running.

Settings shows the exact paths and can create a database snapshot. A database snapshot alone does **not** include attachment bytes or provider configuration. For complete portable folder backups, stop the app and copy the entire folder. On dev, Settings can also create a validated data-and-attachment archive and restore it to a separate directory; see [WORKSPACE_ARCHIVES.md](WORKSPACE_ARCHIVES.md) for exclusions, bounds and launch commands. Restore into a separate folder first. Never copy a live SQLite database without its WAL or use a database snapshot as a full workspace backup.

Windows credentials use DPAPI and are bound to the Windows account. Moving documents across platforms works by copying the stopped workspace, but credentials are not interchangeable across operating systems/accounts; preserve your original folder and reconfigure provider credentials in a new workspace. Never delete keys to force decryption.

## Source builds

Go 1.26 and Node 22+. Windows: build.bat. Unix: ./build.sh. Builds retain existing user data. Executable accepts --root DIRECTORY or --root=DIRECTORY (including spaces), and --stop to send authenticated graceful shutdown for that root. Default launchers always use their own folder, independent of current working directory.

This version is a local workspace foundation. Notion import/export, cloud sync, advanced relation lifecycle and 10k performance acceptance remain tracked in issues #4–#7. Cross-compilation is not native platform acceptance; consult the PR's Linux/macOS/Windows checks before using a package. macOS binaries are currently unsigned; signing/notarization is a remaining distribution limitation.

## Branches

main contains reviewed native-platform-tested releases. dev contains ongoing development. After a verified main release, CI retains only main/dev and deletes other branches only when their commits are ancestors of main or dev; unique work blocks cleanup.
