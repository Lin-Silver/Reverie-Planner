# CI, packaging and release process

Scope of this document: what CI proves, how artifacts are built and verified, what is still missing for macOS signing, and why a green build is not a completion claim. Last reviewed 2026-11-03 on `lane/release` (based on `dev` `6e1d2e65b65cd33e825f83d45cf5972c6e67454e`).

## Branch and CI coverage

`.github/workflows/ci.yml` runs on pushes to `main` and `dev`, on every pull request, and on manual dispatch. Per-job timeouts and npm/Go dependency caching are enabled; PR runs are cancelled when superseded, release-bearing pushes are not. Workflows have read-only repository permissions except the publish job.

| Job | What it runs | Evidence captured |
| --- | --- | --- |
| `build` (windows-latest) | `go test -count=1 ./...`, `go vet ./...`, `build.bat` (npm ci, TypeScript build, Go build), frontend lint, Playwright Chromium suite, Windows start/stop, portable archive + `verify-package.py` | Windows executable and zip |
| `unix-portable` (`ubuntu-latest`, `macos-latest`) | `go test -count=1 ./...`, `go vet ./...`, `build.sh` (npm ci, frontend build, Go build), native portable archive + `verify-package.py` | Linux/macOS tar.gz |
| `release-v001` (`ubuntu-latest`, main pushes only) | downloads all three archives, re-runs `verify-package.py`, publishes the GitHub release **only if `v0.0.1` does not exist** | v0.0.1 release assets |

Current remote evidence (queried from the GitHub API, not inferred from local builds):

- `dev` `6e1d2e65b65cd33e825f83d45cf5972c6e67454e`: [CI run 41426229912](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/41426229912), **success**, 2026-11-02.
- `main` `a6b5923d9c882c12c1a5d36026b310eb6faa1177`: [CI run 40896243774](https://github.com/Lin-Silver/Reverie-Planner/actions/runs/40896243774), **success**, 2026-10-29.
- Published `v0.0.1` (2026-10-29) has 3 assets, all reported by the GitHub API with SHA-256 digests (see [Published artifacts](#published-artifacts)).

Not run for this lane: `lane/release` was never pushed (the task forbids pushing), so the CI changes in this branch have **not** been executed remotely. Their evidence here is local command output plus workflow review. Push and a PR run remain a manual step for the owner.

Coverage gaps deliberately left open: there is no race detector job, no coverage threshold and no performance regression gate. The benchmark workflow is informational and does not fail on a latency threshold. Both the Windows job and the Ubuntu/macOS matrix run lint plus the Playwright suites; only the Windows job adds launcher-ownership and start/stop checks.

## What a green build does and does not prove

A green CI run proves that the exact commit compiled, passed Go tests/vet, produced a frontend build, passed the configured lint and Playwright suite, started/stopped the service, and produced archives that pass structural/checksum verification.

It does **not** prove: macOS Gatekeeper acceptance, real AI-provider calls, manual accessibility/responsiveness acceptance, 10k-row UI rendering, five-layout behavior at scale, migration of real user databases, backup/restore in production conditions, or completion of the database checklist.

### Completion status: 11/35 database checklist

Source of truth: [DATABASE_COMPLETION_PLAN.md](DATABASE_COMPLETION_PLAN.md) (Phases 0–5, 35 checkboxes). Independent count on 2026-11-03: **11 checked, 24 unchecked**.

Completed (11):

1. PR #2 baseline and last green Windows CI recorded (Phase 0).
2. Migration 7 / legacy linked-column handling reviewed (Phase 0).
3. Lazy `ALTER TABLE pages` moved to transactional migration (Phase 0).
4. Backup/restore test uses a real SQLite copy and preserves the source (Phase 0).
5. Deterministic safe formula language with typed rollup aggregations (Phase 1).
6. Table/Board/Calendar/List/Gallery rendering with E2E assertions (Phase 2).
7. One view's filter/sort/Board group/Calendar field persists across reload (Phase 2).
8. Typed nested AND/OR filter AST (Phase 2).
9. Multi-key typed sorting, column layout and keyboard navigation persisted per view (Phase 2).
10. Calendar date drag/month navigation/timezone/unscheduled tray (Phase 2).
11. Record pages open BlockNote, persist content, share linked records (Phase 3).

Open (24): Phase 0 — compatibility tests, API/query normalization (2); Phase 1 — full typed property editors, atomic schema edits, typed validation, indexed relations, attachments (5); Phase 2 — clipboard/bulk edit, Board edge cases, Gallery/List polish, view management UI (4); Phase 3 — title/version coordination, 409 conflict UI, soft-delete lifecycle, restart/fault tests (4); Phase 4 — component split, pagination/virtualization + 10k UI benchmark, accessibility/responsive, cloud independence review (4); Phase 5 — Go regression suite, expanded Playwright, exact-head CI confirmation, parity/verification docs, independent review (5).

The new [10,000-record benchmark](PERFORMANCE_BASELINE.md) supplies the "measurable benchmark" evidence for Phase 4, **but the Phase 4 item remains unchecked** because it also requires pagination/virtualization and the UI at that scale is not measured. Green CI plus a benchmark is still not completion.

## Reproducible build and packaging

Build inputs: Go 1.26.x, Node 22+ with the committed `package-lock.json`, and the same target OS/architecture. `build.bat` and `build.sh` now build with `-trimpath -buildvcs=false`; the frontend bundle is content-addressed.

`scripts/package.py` produces only redistributable files (no `data/`, no private `config/providers.json`, no runtime secrets) and is deterministic: entries are sorted, ZIP timestamps fixed to 1980-01-01, tar mtimes/owners zeroed, gzip header mtime 0. Every archive embeds `BUILD-MANIFEST.txt` with version, target, source commit and the SHA-256 of each payload file.

```powershell
build.bat
python scripts/package.py --target windows-amd64 --source-commit (git rev-parse HEAD)
python scripts/verify-package.py release/Reverie-Planner-0.0.1-windows-amd64.zip --target windows-amd64
```

For Linux/macOS use `./build.sh` and `python3 scripts/package.py --target "$(go env GOOS)-$(go env GOARCH)"`; `verify-package.py` infers the target from the archive name.

### Reproducibility evidence (local, 2026-11-03)

| Check | Result |
| --- | --- |
| Go binary, two consecutive `go build -trimpath -buildvcs=false` | identical SHA-256 `dc4afff1e3fe204e63ab318080a4e1092e8330e070306d1f2a01e2eee643c04f` |
| Frontend `npm run build` twice (11 dist files) | all 11 file hashes identical |
| Windows zip packaged twice (final tree) | identical SHA-256 `e48c58f008feb59efd5866941cc62eb445e28bc45aede52260e5b3c5fa75e086`, 10,878,133 B; verification `ok`, 21 entries, 20 payload files |
| Linux tar.gz packaged twice (binary cross-compiled `GOOS=linux GOARCH=amd64` on Windows, final tree) | identical SHA-256 `b9540c00339b8e4513a38037f62c6dca78f7270a7cdcc56ab65e6500ce36292d`, 10,781,286 B; verification `ok`, 19 entries, 18 payload files |

Limits: reproducibility was verified for one toolchain and one working tree; a different Go patch release, npm/Node version, or target architecture may change bytes. The Linux archive was cross-compiled and structurally verified, not executed natively. macOS was not built here.

### Published artifacts

The existing `v0.0.1` assets were built before this lane (old packaging used filesystem timestamps, and the Windows build lacked `-trimpath/-buildvcs=false`), so their bytes are not expected to match a rebuild from the current tree. Their published SHA-256 digests are:

| Asset | Size | SHA-256 (GitHub API) |
| --- | --- | --- |
| `Reverie-Planner-0.0.1-windows-amd64.zip` | 10,387,106 B | `212d2b33c11030be6a84eb50872e25e86ff94edbb073b80d91a3e22bddca0ef7` |
| `Reverie-Planner-0.0.1-linux-amd64.tar.gz` | 10,323,924 B | `72cedb361f7aba75434c98b39c5f9a718ae664fbbb88b1617f8d8b35e38d09f7` |
| `Reverie-Planner-0.0.1-darwin-arm64.tar.gz` | 9,899,314 B | `cc80dbc8ea0f6a7485aa505acea5c4d421fab7fa33100e21c5f90003880d083d` |

These are historical digests for download integrity, not proof that the current source reproduces them.

## macOS signing and notarization — unsigned, out of scope

Current macOS archives are **unsigned and not notarized**. Signing/notarization is **out of scope for this release by product decision**, not a blocker: there is no `codesign`, `notarytool` or `stapler` step in CI, and none is planned for this release. Gatekeeper will block a downloaded unsigned app unless the user removes quarantine manually; download instructions should state this workaround plainly.

If signing is ever brought back into scope, the project would need credentials that are not present:

1. An Apple Developer Program membership and a **Developer ID Application** certificate exported as `.p12` with its password.
2. A CI keychain import (`security create-keychain`, `security import`, `security set-key-partition-list`).
3. `codesign --force --options runtime --timestamp --entitlements <entitlements.plist>` for the app binary/bundle, plus `spctl --assess` verification.
4. Notarization credentials: either App Store Connect API key (`.p8` file, key ID, issuer ID) or Apple ID + app-specific password + team ID.
5. `xcrun notarytool submit <archive> --wait`, then `xcrun stapler staple <bundle>` and a final `spctl --assess --type execute` check.

Proposed secret names (not configured): `APPLE_CERTIFICATE_P12_BASE64`, `APPLE_CERTIFICATE_PASSWORD`, `APPLE_KEYCHAIN_PASSWORD`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`, `APPLE_API_KEY_P8`, or `APPLE_ID` / `APPLE_APP_SPECIFIC_PASSWORD` / `APPLE_TEAM_ID`. A signing job is intentionally **not** added: it would fail without these credentials and would create a false impression of support.

Manual macOS acceptance (real machine, Gatekeeper, start/stop, data paths) remains a manual step and is not part of this release's automated evidence.

Windows artifacts are likewise unsigned (no Authenticode certificate configured), so SmartScreen warnings are possible. Linux artifacts are unsigned tarballs; integrity relies on the published SHA-256.

## Release procedure

1. Feature work lands on `dev` through a PR; CI must be green on the exact head SHA.
2. After review, `dev` is merged to `main`. A main push reruns the full build/package matrix.
3. `release-v001` verifies the downloaded archives again and publishes only if `v0.0.1` is absent. Publishing is therefore one-shot: a new version requires updating `VERSION` in `scripts/package.py`, the app version, the artifact-name pattern and the release condition/tag.
4. Before announcing, compare the published asset SHA-256 with the verified archive, smoke-test start/stop on at least Windows and Linux, and record the exact CI run URL.
5. Never publish from a lane branch. This lane does not push, tag or create releases.

The release job also performs the repository's existing cleanup of merged branches after a successful release; do not treat that as permission to delete unmerged lane branches.

## Open blockers for this lane

- macOS signing/notarization credentials and real-machine Gatekeeper acceptance are **blocked/manual**.
- This branch has no remote CI run because pushing was prohibited; a PR run is required before merge.
- 10k UI rendering/pagination remains open; the benchmark covers the data layer only.
- No performance threshold gate exists; benchmark output is informational.
- Windows code signing is optional and not configured.
