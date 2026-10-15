# domain — Core Domain Types & Logic

**Location**: `internal/mercato/domain/`

## Purpose

Defines all core types, error codes, frontmatter parsing, sync state, and the install database data model. No external dependencies — pure domain logic.

## Files

| File | Key Types / Functions | Description |
|------|----------------------|-------------|
| `types.go` | `MctRef`, `MctVersion`, `EntryType`, `Market`, `Entry`, `SkillDep`, `Tombstone`, `EntryState`, `Conflict` | Core type definitions |
| `config.go` | `Config`, `MarketConfig`, `NormalizeURL()` | Application configuration model |
| `errors.go` | `DomainError`, error codes (MARKET_NOT_FOUND, ENTRY_NOT_FOUND, INVALID_FRONTMATTER, etc.) | ~20 typed domain errors |
| `frontmatter.go` | `Frontmatter`, `ReadmeFrontmatter`, `ParseFrontmatter()`, `InjectMctFields()`, `PatchMctVersion()`, `PatchMctChecksum()` | YAML frontmatter extraction, injection, patching |
| `state.go` | `SyncState`, `MarketSyncState` | Per-market sync tracking (SHA, timestamp, branch, status) |
| `install.go` | `InstallDatabase`, `InstalledMarket`, `InstalledPackage`, `InstalledLocation`, `InstalledFile`, `InstalledFiles`, `InstallSchemaVersion` | Install DB model (v2 schema) with `FindPackage()`, `FindLocation()`, `AddOrUpdatePackage()` (replace-wholesale), `MergeLocationFiles()`, `MergePackageFiles()` (caller-side composition for incremental adds), `RemoveLocation()`, `CleanStaleLocations()` |
| `runtime_type.go` | `RuntimeTypeClaudeCode`, `RuntimeTypeForDotDir()` | Runtime-type label constants and folder→type lookup used by v1→v2 migration |
| `diff.go` | `DiffAction`, `FileDiff` | Git diff representation (insert/modify/delete) |

## Key Types

### MctRef
Canonical reference: `"market@profile/category/type/name"`. Methods: `Parse()` splits into market + path.

### Entry
Full representation of an agent, skill, command, or hook with metadata, version, state, dependencies, and profile context. `EntryType` constants: `EntryTypeAgent`, `EntryTypeSkill`, `EntryTypeCommand`, `EntryTypeHook`.

### HookSnippet (`hook.go`)
Parsed representation of a `.json` snippet under a market's `hooks/` directory. Fields: `Event`, `Matcher`, `Hooks []json.RawMessage`. Validated by `ParseHookSnippet([]byte) (HookSnippet, error)`. Errors: `ErrInvalidHookSnippet`, `ErrSettingsHooksMalformed`, `ErrConflictHookEventMatcher`.

### EntryState
Enum: `Clean`, `UpdateAvailable`, `Drift`, `UpdateAndDrift`, `Deleted`, `NewInRegistry`, `Orphaned`, `Unknown`, `LocallyDeleted`.

`LocallyDeleted` distinguishes "the user removed the file from disk" from `Drift` ("the user modified the file content"). `Check` returns `Drift` when at least one file is modified (mix takes precedence — modified is more risky than missing) and `LocallyDeleted` only when *every* drifted file is missing. This drives the `mct sync` interactive restore flow.

### Frontmatter
Parsed from YAML header in .md files. Fields: name, description, author, version, tags, deprecated, breaking_change, requires_skills. Mct-injected fields: mct_ref, mct_version, mct_market, mct_profile, mct_installed_at, mct_checksum.

### InstallDatabase (schema v2)
Tracks all installed packages across all projects. Keyed by market name, then by profile. Each package carries:
- `Files` — package-wide skill/agent/command/hook leaf names (drives sync diffs and ref enumeration). `InstalledFiles` has `Skills`, `FlatSkills` (subset of `Skills` installed from a single-file `skills/<name>.md`; `IsFlatSkill(name)`), `Agents`, `Commands`, and `Hooks []string` fields; `MergePackageFiles` unions all of them. For hooks, the per-location `InstalledFile.Path` uses a `<settings.json>#hooks/<file>` suffix so multiple hooks per package keep distinct per-hook checksums.
- `Locations []InstalledLocation` — one entry per (project path, runtime type). The same project path appears multiple times when a package was installed for several runtimes (e.g. claude-code + cursor).

`InstalledLocation { Path, Type, Version, Files []InstalledFile }` — `Version` is the market commit this location was last synced to (empty on legacy DBs; use `InstalledPackage.VersionAt(path)` / `SetVersionAt(path, sha)`, never `pkg.Version` directly). `AddOrUpdatePackage` keeps an existing path's version when adding a sibling entry so pending updates are not skipped. `Type` is `claude-code`, `cursor`, `windsurf`, etc. (taken from `Transformer.ToolName()` for non-claude tools, the constant `RuntimeTypeClaudeCode` for the built-in path). `Files []InstalledFile { Path, XXH, Source, Data }` records every file actually written (`Source` = repo path of the producing entry for tool files; a `Path` containing `#` is a JSON hook patch anchor `<file>#<keys>/<event>` and `Data` holds the exact entry; type `agents` = shared `.agents/` files), with its xxhash64 at install/update time. This is the source of truth for drift detection AND for prune-on-update: each `AddOrUpdatePackage` call replaces the (Path, Type) entry's `Files` wholesale, and the sync flow diffs old vs. new to delete files dropped upstream. For single-entry adds the caller composes the full set with `MergeLocationFiles` (replaces whole records; hook patches keyed by anchor + Source) / `MergePackageFiles` before calling.

`InstallSchemaVersion = 2`. The cfgadapter auto-migrates v1 (`Locations []string` + flat `ToolChecksums` map) on load by walking each location's on-disk `.claude/` tree, hashing every file present, and classifying by the leading dot-folder via `RuntimeTypeForDotDir`. The migrated DB is persisted back so subsequent loads are fast.

## Error Codes

Grouped by category:
- **Market**: MARKET_NOT_FOUND, MARKET_ALREADY_EXISTS, MARKET_URL_EXISTS, MARKET_UNREACHABLE
- **Entry**: ENTRY_NOT_FOUND, ENTRY_ALREADY_INSTALLED, ENTRY_ORPHANED
- **Validation**: INVALID_FRONTMATTER, INVALID_ENTRY_TYPE, MCT_FIELDS_IN_REPO
- **Skill**: SKILL_NOT_FOUND, SKILL_TYPE_MISMATCH, PIN_MISMATCH
- **Sync**: SYNC_DIRTY, CACHE_STALE, OFFLINE_MODE
- **Conflict**: CONFLICT_REF_COLLISION, CONFLICT_DEP_VERSION, CONFLICT_DEP_DELETED
- **Infrastructure**: CLONE_EXISTS, SSH_DISABLED, LOCK_CONTENTION, STALE_LOCK, DRIFT_DETECTED, DEPENDENCY_CYCLE, PATH_TRAVERSAL

## Transformer contract (`transformer.go`)

`Transformer{ToolName, Detect, Transform(TransformInput) TransformResult}`,
`OutputFile`, `HookPatch` (`Anchor()`), `TransformAsset`, `ToolMapping` (user
overrides only), `TransformerRegistry` (`EnabledTransformers` sorted, `Names`),
`RuntimeTypeSharedAgents = "agents"`. Implementations: see
[pkg-domain-transform.md](./pkg-domain-transform.md). `ErrUnknownTool` is
returned for tool names without a transformer.
