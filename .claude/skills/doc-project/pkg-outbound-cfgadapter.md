# outbound/cfgadapter — Config, State & InstallDB Adapter

**Location**: `internal/mercato/outbound/cfgadapter/`

## Purpose

Implements three repository interfaces for persisting configuration (YAML), sync state (JSON), and the install database (JSON with file locking).

## Files

### configstore.go — ConfigStore Implementation

Persists `~/.config/mct/config.yml`.

| Method | Description |
|--------|-------------|
| `Load(path)` | Read and parse YAML config, apply defaults |
| `Save(path, cfg)` | Write YAML config (atomic, 0600 perms) |
| `Exists(path)` | Check if config file exists |
| `AddMarket(path, market)` | Append market to config |
| `RemoveMarket(path, name)` | Remove market from config |
| `SetMarketProperty(path, name, key, value)` | Update a market field |
| `SetConfigField(path, key, value)` | Update a top-level config field |

### statestore.go — StateStore Implementation

Persists `~/.cache/mct/sync-state.json`.

| Method | Description |
|--------|-------------|
| `LoadSyncState(cacheDir)` | Read JSON sync state |
| `SaveSyncState(cacheDir, state)` | Write JSON sync state (atomic temp+rename) |
| `SetMarketSyncDirty(cacheDir, market)` | Mark market sync as incomplete |
| `SetMarketSyncClean(cacheDir, market, sha)` | Record successful sync with SHA |

`SetMarketSyncDirty`/`SetMarketSyncClean` are read-modify-write: they are serialized by an in-process mutex plus the OS file lock on `sync-state.lock` (same mechanism as the install DB), so concurrent updates for different markets are never lost. Lock timeout → `LOCK_CONTENTION`.

### installdb.go — InstallDB Implementation

Persists `~/.cache/mct/installed.json` with file-level locking.

| Method | Description |
|--------|-------------|
| `Load(cacheDir)` | Read installed.json |
| `Save(cacheDir, db)` | Write installed.json (atomic, 0600 perms) |
| `Lock(cacheDir)` | Acquire `installed.lock` (5s timeout) |
| `Unlock(cacheDir)` | Release the lock only if this adapter holds it (no-op otherwise) |

### Locking Strategy (`lockfile.go`, `lockfile_unix.go`, `lockfile_windows.go`)

1. Open (create) `installed.lock` and take an exclusive, non-blocking OS lock on it: `flock(LOCK_EX|LOCK_NB)` on unix, `LockFileEx(EXCLUSIVE|FAIL_IMMEDIATELY)` on Windows. The kernel drops the lock when the holder dies, so there is no stale-PID detection or stale-lock takeover to race on, and an empty/garbage lock file left by a crash is harmless.
2. Unix only: after locking, verify the path still names the locked inode (`os.SameFile`); a holder may have unlinked it meanwhile → retry.
3. Write the PID into the file (diagnostics only, never trusted).
4. Contended → poll until the timeout (5s, 100ms poll) → `LOCK_CONTENTION`.
5. `Unlock` releases only a lock recorded in the adapter's `held` map. On unix the file is removed while still locked, then unlocked; on Windows it is left in place.

Lock acquisition is not re-entrant: a second `Lock` on the same cache dir (even from the same process) waits like any other contender.

## Tool mappings (`toolmappings.go`)

`DefaultToolMappings` is empty: per-tool defaults live in the transformers and
`tool-mappings.yml` only holds user overrides. On load, values identical to
the defaults that older `mct init` wrote (opencode `claude-*-4-*` model ids,
empty cursor/windsurf/codex/gemini entries, lowercase opencode tool names) are
dropped so they no longer shadow the transformer defaults.
