# outbound/gitadapter — Git Adapter

**Location**: `internal/mercato/outbound/gitadapter/`

## Purpose

Implements the `GitRepo` interface using go-git. Handles all Git operations: clone, fetch, diff, file reading, and remote validation.

## Constructor

```go
New(opts ...Option) *GitAdapter
```

### Options
- `WithSSHEnabled()` — Enable SSH authentication
- `WithDepth(n)` — Set shallow clone depth (default: 1)
- `WithProgress(w)` — Send clone/fetch transfer progress to `w`. Default is `io.Discard` so progress never pollutes `--json` stdout or the TUI

### Concurrency

The adapter keeps a `sync.RWMutex` per clone directory (`cloneLocks`). `Clone` and `Fetch` take the write lock; every read method takes the read lock. go-git rewrites ref files in place (truncate + write) during `Fetch`, so unguarded concurrent readers (e.g. a TUI search during a refresh) intermittently fail with "object not found" / "repository does not exist". The lock is in-process only: a separate `mct` process fetching the same clone is not covered.

## Files

### gitadapter.go — Core Implementation

| Method | Description |
|--------|-------------|
| `Clone(url, path, branch)` | Shallow clone with optional SSH auth, HTTP auth via git credential helpers |
| `DefaultBranch(path)` | Branch the clone tracks: symbolic HEAD, else `origin/HEAD`, else the only `origin/*` or local branch, else probe `main` then `master`; errors if none resolve (HEAD is detached after `Fetch`) |
| `Fetch(path, branch)` | Fetch updates from remote |
| `DiffSinceCommit(path, sha)` | Compute file changes since a commit |
| `ReadFileAtRef(path, ref, file)` | Read file content at specific commit/branch |
| `FileVersion(path, file)` | Last commit (first-parent walk from HEAD) that changed the file; stops at the shallow boundary. Returns `gitrepo.ErrFileNotFound` when the file is absent from HEAD |
| `RemoteHEAD(url)` | Get remote HEAD SHA without cloning |
| `ListFiles(path)` | List all .md files in repo |
| `IsValidRepo(path)` | Check if path contains a valid git repo |
| `ValidateRemote(url)` | Validate URL is reachable before cloning |
| `ReadMarketFiles(path, branch)` | Read all market entries in one pass |
| `ListDirFiles(path, dir, ref)` | List files in a specific directory at a ref |

### credentials.go — HTTP(S) credentials

go-git ignores git's credential helpers, so `Clone`, `Fetch` and `ValidateRemote` run through `withHTTPCredentials`: the first attempt is anonymous; on an HTTP 401/403 for an `http(s)://` URL it runs `git credential fill` (configured helpers, `GIT_ASKPASS`, `core.askPass`; `GIT_TERMINAL_PROMPT=0` so it never prompts on the TTY), retries once with Basic auth, then `git credential approve` on success or `reject` on a new auth failure — same contract as `git clone`. If git is missing or no credential is found, the original auth error is returned.

### helpers.go — SSH & Git Utilities

| Function | Description |
|----------|-------------|
| `isSSHURL(url)` | Detect SSH URL format |
| `resolveAuth(url)` | Build SSH auth for clone operations |
| `remoteURL(repo)` | Origin remote URL of an opened repo (empty if none) |
| SSH key loading | Tries `~/.ssh/` keys: ed25519, ecdsa, rsa |
| SSH config parsing | Reads `~/.ssh/config` for per-host IdentityFile |
| SSH agent support | Loads from `SSH_AUTH_SOCK` |
| Known hosts | Validates against `~/.ssh/known_hosts` |
| Tree walking | Helpers for traversing git trees and computing diffs |
