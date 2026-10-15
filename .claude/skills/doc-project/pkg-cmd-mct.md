# cmd/mct — Entry Point

**Location**: `cmd/mct/main.go`

## Purpose

Application bootstrap. Resolves config/cache directories, creates all adapters, wires dependency injection, and builds the root Cobra command.

## Key Functions

- `main()` — Resolves `~/.config/mct/` and `~/.cache/mct/` paths, calls `NewApp()` to build the CLI, then executes
- `NewApp(configPath, cacheDir)` (in `init.go`) — Creates all outbound adapters (git, fs, config, state, installdb), constructs the `App`, and assembles the root command with all subcommands
- `resolveSSHEnabled()` — Checks `MCT_SSH_ENABLED` env var, then config file, defaults to false
- `newTUICmd()` — Registers the TUI subcommand

## DI Wiring

```
gitadapter.New(opts...) → GitRepo
fsadapter.New()         → Filesystem
cfgadapter (config, state, installdb) → ConfigStore, StateStore, InstallDB
app.New(git, fs, cfg, state, idb, paths) → App (implements all service interfaces)
commands.NewRootCmd(app) → cobra.Command
```

## Files

| File | Description |
|------|-------------|
| `cmd/mct/main.go` | Entry point, directory resolution |
| `internal/mercato/init.go` | `NewApp()` constructor, adapter creation, TUI command |
| `internal/mercato/assets/embed.go` | Embedded FS placeholder |

## Self-update (`internal/mercato/update`)

- Daily background check (`ShouldCheck` / `CheckLatestVersion`, state in `<cacheDir>/update-check.json`) queries `api.github.com/repos/JLugagne/agents-mercato/releases/latest`; `newerVersion` compares semver (a `dev` build is always older).
- `RunUpgrade` (`mct upgrade` / `dist-upgrade`) is an in-house updater (`upgrader.go`, no third-party self-update library): download `mct_<os>_<arch>.tar.gz` + `checksums.txt`, verify SHA-256, extract `mct`, write a temp file next to the binary and rename over it.
- Release signing: when `update/signing_key.asc` (go:embed, empty by default) holds an armored public key, `checksums.txt.sig` is required and verified with `github.com/ProtonMail/go-crypto/openpgp` (armored or binary detached signature). The release workflow signs `checksums.txt` only when the `GPG_PRIVATE_KEY` / `GPG_PASSPHRASE` secrets exist.
