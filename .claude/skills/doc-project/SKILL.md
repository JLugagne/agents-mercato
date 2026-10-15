---
name: doc-project
description: >
  ALWAYS LOAD THIS SKILL when working on agents-mercato. Comprehensive project documentation:
  purpose, how it works end-to-end, architecture, all packages, data models, and key workflows.
  Required context for any code change, review, or question about this codebase.
---

# agents-mercato (mct) — Project Documentation

## What is agents-mercato?

agents-mercato (`mct`) is a **decentralized package manager for Claude Code agents and skills**. It solves the problem of distributing, versioning, and synchronizing Claude agent/skill definitions across teams and projects — without requiring any central registry or infrastructure.

### The Problem

Claude Code agents and skills are markdown files (`.md` / `SKILL.md`) that live in a project's `.claude/` directory. Teams need to:
- Share curated sets of agents and skills across multiple projects
- Keep those definitions in sync when upstream authors publish updates
- Detect when someone locally modifies an installed skill (drift detection)
- Manage dependencies between skills (skill A requires skill B)
- Avoid conflicts when multiple sources provide overlapping definitions

### The Solution

mct uses **Git repositories as distribution channels** (called "markets"). A market is just a Git repo with a conventional directory structure containing agent and skill files. mct clones markets locally, indexes their contents, and lets users install entries into their projects. It then tracks versions, detects drift, resolves dependencies, and synchronizes updates — all through Git.

### How It Works End-to-End

1. **Register a market**: `mct market add <git-url>` — mct shallow-clones the repo into `~/.cache/mct/<name>/`, auto-detects its format (hierarchical or flat), and saves it to `~/.config/mct/config.yml`

2. **Browse & search**: `mct search <query>` or `mct tui` — mct builds a BM25 full-text index from all market entries, supports fuzzy matching, filtering by type/market/category, and an interactive TUI with three-panel browsing

3. **Install an entry**: `mct add <market>@<path>` — mct reads the file from the cached clone, validates its YAML frontmatter, recursively resolves skill dependencies (with cycle detection), injects tracking metadata (`mct_ref`, `mct_version`, `mct_checksum`, etc.), writes files to `.claude/`, and records the installation in `~/.cache/mct/installed.json`

4. **Check status**: `mct check` — for every installed entry, mct computes its state by comparing the local file hash (xxhash) against both the recorded version and the latest upstream version. `update_available` is reported only when the diff between the location's recorded commit and HEAD touches one of the package's own files (unrelated market commits do not flag it; a recorded commit missing from the clone counts as changed). States: clean, update_available, drift, locally_deleted, update_and_drift, deleted, orphaned

5. **Sync updates**: `mct sync` (or `mct refresh` + `mct update` separately) — fetches from all markets, prunes stale install locations and upstream-removed files, then offers to **restore any locally-deleted file** interactively (`[r]/[k]/[a]/[n]` prompt; `--restore-all` / `--restore-none` flags; CI mode skips silently), and finally applies updates. Drift conflicts are handled via policy flags (`--all-keep`, `--all-merge`). `--dry-run` reports `would_update` results without writing. Upstream removals are only pruned when git confirms the file is absent (`gitrepo.ErrFileNotFound`); a git failure never deletes local files

   Before updating, `update`/`sync` run `mct migrate` for the project: files written by older mct versions (extension-less single-file skills, legacy hook entries, previous tool formats, removed tools) are converted; locally modified files are kept. `mct migrate --dry-run --all-locations` previews every project.

6. **Audit health**: `mct doctor` — read-only, offline. Surfaces every issue in one shot: drift, locally-deleted files, stale locations, upstream-removed files, orphaned packages. JSON output via `--json` for CI tooling. Run `mct refresh` first if you need a fresh upstream view.

7. **Export/restore**: `mct save` writes a `.mct.json` manifest; `mct restore` re-installs from it. Combined with optional Git hooks (`mct hook install`), this enables fully automatic save-on-push and restore-on-pull workflows

### Market Format

**Hierarchical** (full market):
```
market-repo/
  profile-a/category-x/
    README.md              ← profile metadata (tags, description)
    agents/agent1.md       ← agent definition
    skills/skill1/SKILL.md ← skill definition (+ supporting files)
  profile-b/category-y/
    README.md
    agents/agent2.md
```

**Flat** (skills-only repo):
```
repo/
  skills/
    skill1/SKILL.md
    skill2/SKILL.md
```

Format is auto-detected on `market add`. The first two path segments form a "profile" that groups related entries.

### Entry Type Inference

Determined by path convention, not declaration:
- `.md` file under `agents/` → Agent (installed to `.claude/agents/<name>.md`)
- `SKILL.md` under `skills/<name>/` → Skill (installed to `.claude/skills/<name>/`)
- Single-file `skills/<name>.md` → Skill (installed to `.claude/skills/<name>/SKILL.md`, recorded in `InstalledFiles.FlatSkills`; legacy extension-less `.claude/skills/<name>` files are replaced on the next add/update)
- `.md` file under `commands/` → Command (installed to `.claude/commands/<name>.md`)
- `.json` file under `hooks/` → Hook (merged into `.claude/settings.json`)
- Everything else → Ignored by mct

Commands are flat single-file entries (like agents). Other tools receive them converted (OpenCode/Kilo/pi/Gemini/Qwen/Factory/Junie command files, Windsurf/Cline workflows, or a user-invocable skill for Cursor/Copilot/Kiro/Vibe/Antigravity/DeepCode). Commands support `requires_skills` in frontmatter, resolved via the same dependency engine as skills.

Hooks are JSON snippets, NOT files copied to disk. Install adds the snippet as one Claude Code matcher group `{"matcher": <snippet matcher>, "hooks": [<handlers>]}` under `hooks[<event>]` in `.claude/settings.json`. Claude Code's settings schema rejects unknown keys on matcher groups and handlers, so nothing mct-specific is written there: ownership lives in the sidecar `.claude/mct-hooks.json` (`mct_id` → `{ref, event, matcher, hooks}`, `mct_id` = xxhash of the ref). An owned group is located by exact (event, matcher, handlers) match, falling back to the single unclaimed group with the same matcher and handler types (ambiguity → unresolved, so user groups are never touched). Legacy flat entries (`{type, command, matcher, mct_id}` directly under the event, written by older mct versions) are migrated to groups + sidecar on any install/remove/update; user-owned entries are left as is. Drift compares a canonical checksum of the handlers (group matcher folded in, so pre-migration checksums stay valid) against the recorded value: reformatting is not drift, command edits are. Two markets providing hooks with the same `(event, matcher)` are blocked at install with `ErrConflictHookEventMatcher`; re-installing the same ref replaces its group. Note: `mct hook install` (the existing CLI command) installs **git** hooks for save/restore automation — unrelated to the new `hook` entry type.

### Dependency Resolution

Skills can declare dependencies in their frontmatter:
```yaml
requires_skills:
  - file: skills/foo/SKILL.md              # same-market dep
  - file: skills/bar/SKILL.md
    market: https://github.com/org/other.git  # cross-market dep
    pin: abc123                               # optional version pin
```

mct resolves dependencies recursively with cycle detection. Cross-market deps auto-register the foreign market if not already known (with user confirmation in interactive mode).

### Drift Detection

After installation, mct tracks each file's content hash (xxhash64). On `mct check`, it compares:
- Local file content vs. original content at the recorded git version
- If they differ → the user has locally modified the file → marked as "drift"

This lets users customize installed skills while still being warned about upstream updates that may conflict.

### Conflict Detection

Two types:
1. **Ref collision** — Same filename exists in multiple markets (ambiguous install)
2. **Version mismatch** — Same skill required at different versions by different agents

### Project Isolation

Files are **copied, not symlinked**. Each project gets its own independent copy in `.claude/`. The install database (`installed.json`) tracks which projects have which entries, enabling multi-project cleanup via `CleanStaleLocations()`.

## Tech Stack

- **Go 1.26** with **Cobra** (CLI), **Charmbracelet** (TUI), **go-git** (Git ops)
- **BM25 + Snowball** for full-text search, **xxhash** for drift detection, **YAML** for config

## Architecture

Hexagonal (Ports & Adapters):

```
cmd/mct/main.go          → entry point, wires DI
inbound/commands/         → Cobra CLI handlers
inbound/queries/tui/      → Bubble Tea interactive UI
app/                      → business logic (implements all service interfaces)
domain/                   → types, errors, frontmatter parsing, state
domain/service/           → port interfaces (queries + commands)
domain/repositories/      → adapter interfaces (git, fs, config, state, installdb, tx)
outbound/gitadapter/      → go-git implementation
outbound/fsadapter/       → OS filesystem implementation
outbound/cfgadapter/      → YAML config, JSON state, install DB with file locking
outbound/txadapter/       → disk-backed transactional staging for atomic install/update/remove
```

### Atomicity (transactional install/update/remove)

Every mct write path (`Add`, `Update`, `Remove`, `Prune`) opens a per-package
`tx.Tx` from the `tx.Manager` port. All file writes, deletes, and the install
database save are buffered in a per-operation staging directory under
`<cacheDir>/staging/<txID>/`. On `Commit` the manager applies the staged
operations **in call order** (so `DeleteAll(dir)` then `WriteFile(dir/x)`
keeps `x`): writes are promoted by rename (copy+remove cross-fs), and a
`progress` counter is persisted after each operation. On `Rollback` nothing
on disk has changed. A `Commit` that fails while the process is alive marks
the tx `failed` and removes its staging dir: it is never replayed, since later
operations may already have rewritten the same targets (e.g. `installed.json`).
Only a `committing` dir left by a process that died mid-commit is replayed by
the startup recovery pass (`App.RecoverPendingTx`, under the install DB lock),
resuming from `progress`.

## Package Index

| Package | Role | Details |
|---------|------|---------|
| [cmd/mct](./pkg-cmd-mct.md) | Entry point | Bootstrap, DI wiring, root command |
| [domain](./pkg-domain.md) | Core types & logic | Config, types, errors, frontmatter, state, install DB, diffs |
| [domain/service](./pkg-domain-service.md) | Port interfaces | Market, Entry, Sync, Search, Readme, Config contracts |
| [domain/repositories](./pkg-domain-repositories.md) | Adapter interfaces | Filesystem, GitRepo, ConfigStore, StateStore, InstallDB |
| [app](./pkg-app.md) | Application layer | Market mgmt, entry install/remove, sync, search, lint, conflicts |
| [inbound/commands](./pkg-inbound-commands.md) | CLI commands | All Cobra subcommands, JSON output, global flags |
| [inbound/queries/tui](./pkg-inbound-tui.md) | Terminal UI | Bubble Tea app with panels, search, command mode |
| [outbound/gitadapter](./pkg-outbound-gitadapter.md) | Git adapter | go-git clone/fetch/diff/read, SSH auth |
| [outbound/fsadapter](./pkg-outbound-fsadapter.md) | Filesystem adapter | Read/write/symlink, MD5 checksums |
| [outbound/cfgadapter](./pkg-outbound-cfgadapter.md) | Config/state adapter | YAML config, JSON state/installdb, file locking |
| [domain/transform](./pkg-domain-transform.md) | Multi-tool targets | 22 transformers (opencode, codex, cursor, copilot, gemini, pi, kilo, factory, qwen, kiro, …), shared `.agents/skills`, hook conversion |
| outbound/txadapter | Tx adapter | Disk-backed staging dir + commit/rollback for atomic install/update/remove |

### Multi-tool targets

Entries are authored for Claude Code; `domain/transform` converts them for
the tools enabled in config (`tools.<name>: true`, `--tools a,b`) and detected
in the project (marker dir such as `.codex/`). Unknown tool names are
rejected (`ErrUnknownTool`). Skills that several tools read go once to
`.agents/skills/<name>/` (location type `agents`); tools that read
`.claude/*` natively get nothing. Hooks become JSON patches inside the tool's
own settings/hooks file (the exact entry is recorded in the install DB so it
can be spliced out later), per-hook files, or generated TypeScript plugins
(OpenCode, Kilo, pi). Every written artefact is recorded per tool with its
`Source` entry; removal, drift and update work from those records, and an
update deletes artefacts of disabled or removed tools. See
[pkg-domain-transform.md](./pkg-domain-transform.md).

## Key Concepts

- **Market** — A Git repo containing agent/skill definitions in a hierarchical or flat layout
- **Entry** — An agent (.md under `agents/`), skill (`SKILL.md` under `skills/<name>/`), command (.md under `commands/`), or hook (.json under `hooks/`, merged into `.claude/settings.json`)
- **Profile** — First two path segments group entries (e.g., `dev/go`)
- **MctRef** — Canonical reference in `market@path` format
- **Drift** — Local modifications detected via xxhash comparison against upstream
- **Sync** — Fetch upstream changes, compute state (clean/update/drift/deleted), apply updates
- **Dependency resolution** — Skills can require other skills, even cross-market, with cycle detection
- **Conflict detection** — Filename collisions across markets, version mismatches on shared deps
- **Git hooks** — Optional post-merge (`mct restore`) and pre-push (`mct save`) automation

## Data Flow

```
User runs `mct add market@dev/go/skills/foo`
  → CLI parses ref → App.Add()
    → GitRepo.ReadFileAtRef() reads from cached clone
    → Frontmatter.Parse() validates YAML header
    → Dependency resolution (recursive, cycle-safe)
    → InjectMctFields() adds tracking metadata
    → Filesystem.WriteFile() writes to .claude/
    → InstallDB.Save() records in installed.json
```

## Configuration

- **Config**: `~/.config/mct/config.yml` — markets, policies (conflict, drift), local path, SSH
- **Cache**: `~/.cache/mct/` — shallow clones, `installed.json`, `sync-state.json`
- **Env vars**: `MCT_SSH_ENABLED`, `MCT_LOCAL_PATH`, `MCT_CONFLICT_POLICY`, `MCT_DRIFT_POLICY`
