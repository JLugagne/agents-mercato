# app — Application Logic Layer

**Location**: `internal/mercato/app/`

## Purpose

The central business logic layer. The `App` struct implements all service interfaces from `domain/service/` and orchestrates domain types with repository adapters.

## App Struct

```go
type App struct {
    git          gitrepo.GitRepo
    fs           filesystem.Filesystem
    cfg          configstore.ConfigStore
    state        statestore.StateStore
    idb          installdb.InstallDB
    configPath   string
    cacheDir     string
    transformers domain.TransformerRegistry
    toolMappings configstore.ToolMappingStore
    txm          tx.Manager  // staging-dir tx manager (defaults to passthrough)
}
```

### Transactional install/update/remove

Every write path opens a `tx.Tx` via `App.beginWriter(op)`. Writes go through
a `txWriter` (wrapping the tx); reads continue to use `a.fs` directly. The
install database save is staged via `App.stageDBSave(w, db)` — `idb.Marshal`
+ `tx.WriteFile(idb.Path(...), data)` — so the DB lands atomically with the
file changes. `commit()` promotes everything; on any error the deferred
`rollback()` discards the staging dir.

Granularity is **per-package**: each top-level `Add`/`Update`-at-location/
`Remove`/`Prune` opens its own transaction. Recursive dependency installs
each get their own transaction too, so partial-progress on a multi-skill
add is preserved.

## Files

### market.go — Market Management

Implements `MarketQueries` + `MarketCommands`.

| Method | Description |
|--------|-------------|
| `ListMarkets()` | Load config, convert MarketConfig to Market types |
| `GetMarket(name)` | Find specific market by name |
| `MarketInfo(name)` | Market details with entry counts and sync status |
| `AddMarket(url, opts)` | Clone repo (shallow), validate structure, auto-detect format, add to config |
| `RemoveMarket(name, opts)` | Remove from config, optionally delete cached clone |
| `SetMarketProperty()` | Update a market field in config |

Helpers: `marketNameFromURL()`, `isSkillPath()`, `marketDirName()` (converts `a/b` to `a--b`), `projectPath()`, `findMarketConfig()`

### entry.go — Entry Install/Remove

Implements `EntryQueries` + `EntryCommands`.

| Method | Description |
|--------|-------------|
| `List(opts)` | List installed entries at current project location |
| `GetEntry(ref)` | Get specific entry by MctRef |
| `ReadEntryContent()` | Read raw file from market repo |
| `ListSkillDirFiles()` | List all files in a skill directory |
| `Add(ref, opts)` | Install entry + resolve dependencies recursively, inject mct fields, write to disk, update installdb |
| `Remove(ref, opts)` | Uninstall from current location, update installdb |
| `Prune(opts)` | Clean up deleted upstream entries |
| `Init(opts)` | Create `.claude/` directory, initialize config |
| `Conflicts()` | Detect filename collisions across markets |

### sync.go — Synchronization

Implements `SyncQueries` + `SyncCommands`.

| Method | Description |
|--------|-------------|
| `Check(opts)` | Compute EntryState for all installed entries (clean/update/drift/deleted/orphaned) |
| `Refresh(opts)` | Prune stale install-DB locations, fetch from each market, then for each fetched market run upstream-removed file pruning. Surfaces `PrunedLocations` and `PrunedFiles` in `RefreshResult`. |
| `pruneStaleLocations()` | DB-only cleanup: walks `installed.json`, drops every `Location` whose project dir no longer exists (uses `InstallDatabase.RemoveLocation` cascade), then stages an atomic DB save via the tx writer. No file deletion — files are already gone. Skipped on `DryRun`. Uses `a.fs.Stat` so the check is fully fakeable. |
| `pruneRemovedUpstreamFiles(market, branch, clonePath)` | Per market, after fetch: walks every installed package, checks `pkg.Files.{Skills,Agents,Commands,Hooks}` against the clone at HEAD. Each missing entry is deleted on disk across **all** locations (skills via `DeleteAll`, agents/commands via `DeleteFile`, hooks via `removeHookSnippet` to splice `settings.json` by `mct_id`), removed from `pkg.Files`, removed from each `loc.Files`, and packages that become empty are dropped. Atomic via the tx writer. Skipped on `DryRun`. |
| `detectDriftSplit(pkg, location, clonePath, branch)` | Single source of truth for drift detection — returns `(modified, deleted []string)`. Modified = file present but xxhash mismatches recorded value. Deleted = file is `os.IsNotExist` (user removed it). All filesystem reads go through `a.fs` so v2 (per-location hash) and legacy (git-comparison) paths share identical fakeable I/O. `detectDrift` is now a thin wrapper returning the union for legacy callers (`Update`). |
| `DetectDeleted(opts)` | Read-only scan of installed files that are missing on disk. Excludes hook entries (their "presence" is in settings.json, not a standalone file). Returns `[]DeletedFile` with `(market, profile, location, relPath, xxh)`. Used by `mct sync` between Refresh and Update to drive the interactive restore prompt. |
| `RestoreDeleted(files, opts)` | Reinstalls each `DeletedFile` by reading its content from the cached clone at the recorded `pkg.Version` and writing it back through the tx writer. Groups by `(market, profile, location)` so each package commit is atomic. `DryRun` returns `would_restore` actions without writing. Failures: `errMarketNotFound`, `errPackageNotFound`, `errUnknownFileShape`. |
| `Doctor(opts)` | Read-only health audit. Categorizes findings into `ModifiedFiles`, `LocallyDeleted`, `StaleLocations`, `UpstreamRemoved`, `OrphanedPackages`. Reuses `detectDriftSplit` (modified/deleted), `a.fs.Stat` (stale), `git.FileVersion`/`ListDirFiles` (upstream — uses cached clone, no fetch), and a config-vs-DB diff (orphans). Never writes anywhere; `DoctorReport.HasFindings()` for CI/exit-code use. |
| `Update(opts)` | Apply pending changes with drift/conflict handling. When `AllLocations` is false (default), only updates the current project location; when true, updates all installed locations. `copyUpdatedFiles()` writes skill/agent/command/hook files to each location's `.claude/` directory. |
| `copyUpdatedFiles(w, ctx)` | Reads updated files from the cached clone at HEAD and writes them to the local project — paths are built from `ctx.location/.claude/{skills,agents,commands,settings.json}` so each installed location gets the correct files. |
| `Sync(opts)` | Combined Refresh + Update |
| `detectDrift()` | Compare local file xxhash vs upstream content at recorded version |
| `pruneRemovedFiles()` | Diff old vs. new `[]InstalledFile`, delete `old\new` from disk, walk parent dirs and remove any that become empty (stops at project root). Called by `updatePackageAtLocation` (claude-code) and `reTransformToolTargets` (per tool) so files dropped upstream don't linger. |

### search.go — Full-Text Search

Implements `SearchQueries`.

| Method | Description |
|--------|-------------|
| `Search(query, opts)` | BM25 search with Snowball stemming, fuzzy matching (Levenshtein <= 2), tag boosting (3x) |
| `DumpIndex()` | Export all entries |
| `BenchIndex()` | Performance metrics |

Helpers: `buildCorpus()`, `filterEntries()`, `buildProfileDoc()`, `tokenize()`, `expandFuzzy()`

### lint.go — Market Validation

| Method | Description |
|--------|-------------|
| `LintMarket(name)` | Validate frontmatter, path structure, README presence, skill deps |

### conflict.go — Collision Detection

| Method | Description |
|--------|-------------|
| `Conflicts()` | Find files with same name across markets |

### readme.go — README Queries

| Method | Description |
|--------|-------------|
| `Readme(market, path)` | Read specific profile README |
| `ListReadmes(market)` | List all READMEs in market |
