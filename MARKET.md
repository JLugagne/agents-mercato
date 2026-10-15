# Creating a Market

A market is a Git repository with a specific directory structure. `mct` indexes it to let users discover, install, and update agent, skill, slash command, and Claude Code hook definitions — and to **transform** them into the native format of any supported AI coding tool (Claude Code, Codex, Cursor, Copilot, Gemini, OpenCode, pi, Kilo, Factory, Qwen, Kiro, Windsurf, Junie, Vibe, Antigravity and more — see the README).

`mct` supports two repository formats: the **mct market format** (hierarchical, with profiles) and the **skills-only format** (flat, for pure skill collections). When you register a repository, `mct` auto-detects which format it uses.

> Market authors only need to write entries **once**, in the canonical Markdown + YAML frontmatter format described below. `mct` handles the per-tool conversion (frontmatter, file extension, output directory) at install time. See [Tool transformations](#tool-transformations) for the per-tool output mapping.

---

## mct market format

### Repository structure

```
your-market/
  profile-a/
    category-x/
      README.md              # Profile description and tags
      agents/
        my-agent.md          # Agent definition
      skills/
        helper-skill/
          SKILL.md            # Skill definition
          other-file.txt      # Additional files (bundled with the skill)
      commands/
        my-command.md        # Slash command definition (Claude Code only)
      hooks/
        go-vet.json          # Hook snippet (merged into .claude/settings.json)
  profile-b/
    category-y/
      README.md
      agents/
        another-agent.md
      skills/
        another-skill/
          SKILL.md
```

Key rules:

- Agent files must be `.md` and live under an `agents/` directory.
- Skill definitions use a directory structure: `skills/<name>/SKILL.md`. All files in the skill directory are bundled — not just `SKILL.md`.
- Command files must be `.md` and live under a `commands/` directory. Slash commands are Claude-Code-specific; other tools silently skip them on install.
- Hook snippets must be `.json` and live under a `hooks/` directory. Hooks are merged into `.claude/settings.json` rather than copied as files; they are Claude-Code-specific.
- The first two path segments form the **profile** (e.g. `profile-a/category-x`).
- A `README.md` at the profile level (`profile/category/README.md`) provides metadata for all entries in that profile.
- Everything else is ignored by `mct`.

### Entry type inference

The entry type is **inferred from the path**, not declared in frontmatter:

- `.md` file under `agents/` → **Agent**
- `SKILL.md` (or any `.md`) under `skills/<name>/` → **Skill**
- `.md` file under `commands/` → **Command**
- `.json` file under `hooks/` → **Hook**

If `mct` cannot determine the type from the path, the file is ignored (and `mct lint` will flag it as an error).

### Entry frontmatter

Every agent, skill, or command file must start with YAML frontmatter delimited by `---` (hooks are JSON snippets and have no frontmatter — see [Hook snippets](#hook-snippets) below):

```yaml
---
description: Short description of what this does
author: Your Name
---

Your agent/skill prompt content here...
```

#### Required fields

| Field | Type | Description |
|-------|------|-------------|
| `description` | string | Short description, used in search results, the TUI, and as the description in tool-specific frontmatter |

#### Optional fields

| Field | Type | Description |
|-------|------|-------------|
| `author` | string | Author name |
| `breaking_change` | bool | Flags a breaking change in this version. Users who haven't passed `--accept-breaking` will be prompted before updating. |
| `deprecated` | bool | Marks entry as deprecated |
| `requires_skills` | list | Skills this entry depends on (see [Skill dependencies](#skill-dependencies) below) |

### Skill dependencies

An agent, skill, or command can declare that it requires other skills. When a user installs this entry, `mct` auto-installs all required skills — with cycle detection to prevent infinite loops.

```yaml
---
description: Go developer agent
requires_skills:
  - file: skills/go-test/SKILL.md
  - file: skills/go-lint/SKILL.md
    pin: abc1234
---
```

Each dependency entry supports these fields:

| Field | Required | Description |
|-------|----------|-------------|
| `file` | yes | Path to the skill file, relative to the market root. Must be a safe relative path — no absolute paths, no `..` traversal, and only alphanumerics, hyphens, underscores, dots, and forward slashes. |
| `pin` | no | Locks the dependency to a specific commit SHA |
| `market` | no | URL of an external market containing the skill (see below) |

The `file` path can point to either a `SKILL.md` file (e.g. `skills/go-test/SKILL.md`) or a skill directory (e.g. `skills/go-test`), which `mct` normalizes to `skills/go-test/SKILL.md` automatically.

#### Cross-market dependencies

Skills can depend on skills from other markets:

```yaml
---
description: Full-stack developer agent
requires_skills:
  - file: skills/go-test/SKILL.md
  - file: skills/python-lint/SKILL.md
    market: https://github.com/acme/python-skills
    pin: def5678
---
```

When a `market` URL is specified and the market isn't already registered, `mct` prompts the user for confirmation and then auto-registers it. In non-interactive mode (`--ci`), unregistered cross-market dependencies are rejected.

### Profile README

A `README.md` at the profile level (`profile/category/README.md`) can have its own frontmatter:

```yaml
---
description: Tools for Go development
tags:
  - golang
  - testing
  - development
---

# Go Development Profile

This profile contains agents and skills for Go development workflows...
```

| Field | Type | Description |
|-------|------|-------------|
| `description` | string | Shown in the TUI profile list and used as a fallback description in search results |
| `tags` | list of strings | Boosted in search ranking (repeated 3× internally for BM25 term frequency) |

The README body text is also indexed for full-text search, so any context you write there improves discoverability.

### Profile-level install

Users can install an entire profile in one command:

```bash
mct add acme/agents@dev/go
```

This installs all agents, skills, commands, and hooks under the `dev/go` profile. Already-installed entries are skipped.

### Example market

```
acme-market/
  dev/
    go/
      README.md
      agents/
        go-developer.md
        go-reviewer.md
      skills/
        go-test/
          SKILL.md
        go-lint/
          SKILL.md
      commands/
        ship.md
      hooks/
        go-vet-pre-tool.json
    python/
      README.md
      agents/
        python-developer.md
      skills/
        pytest/
          SKILL.md
  ops/
    docker/
      README.md
      agents/
        dockerfile-writer.md
```

This market has 3 profiles (`dev/go`, `dev/python`, `ops/docker`) containing 5 agents, 3 skills, 1 command, and 1 hook.

---

## Commands

Slash commands are markdown files under a `commands/` directory in any profile (or at the repo root in a flat market). They appear as `/command-name` inside Claude Code.

```
my-market/
  dev/go/
    commands/
      review.md
      ship.md
```

Command files use the same YAML frontmatter as agents and skills, including optional `requires_skills`:

```markdown
---
description: Run the project's pre-flight checks before shipping
requires_skills:
  - file: skills/go-test/SKILL.md
---

Run `go test ./...`, `go vet ./...`, and `golangci-lint run` and report failures.
```

When installed, a command is copied to `.claude/commands/<filename>.md` in the target project. Required skills are resolved and installed alongside.

**Commands are Claude-Code-specific.** Other transform targets (Cursor, Codex, Gemini, etc.) silently skip command entries.

---

## Hooks

Hooks let a market distribute Claude Code hook configurations (e.g. an auto-`go vet` before every Bash tool call). They live as `.json` files under a `hooks/` directory:

```
my-market/
  dev/go/
    hooks/
      go-vet-pre-tool.json
      lint-on-stop.json
```

### Hook snippets

Each file is a JSON snippet describing a single hook configuration:

```json
{
  "event": "PreToolUse",
  "matcher": "Bash",
  "hooks": [
    { "type": "command", "command": "go vet ./..." }
  ]
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `event` | yes | The Claude Code lifecycle event (e.g. `PreToolUse`, `PostToolUse`, `Stop`, `UserPromptSubmit`) |
| `matcher` | no | Tool-name matcher; empty matcher acts as wildcard |
| `hooks` | yes | Array of hook objects (each must have at least `type`) |

### Install behavior

Hooks are **not** copied as files. On `mct add`, `mct` reads the snippet, injects an `mct_id` field (xxhash of the ref) into every hook object, and **merges** them into `.claude/settings.json` under `hooks[<event>]`. All other settings.json keys are preserved verbatim.

On `mct remove`, only the entries whose `mct_id` matches the recorded ref are spliced out — user-authored hooks and hooks from other markets stay untouched.

### Drift detection

`mct check` finds the recorded `mct_id` in the live settings.json and compares the canonical body checksum (with `mct_id` excluded) against the value recorded at install. User reformatting (key reorder, whitespace) does not register as drift; command edits do.

### Conflicts

If two markets provide hooks with the same `(event, matcher)` pair, the second install is rejected with `CONFLICT_HOOK_EVENT_MATCHER`. Hooks are intentionally `(event, matcher)`-unique across markets.

**Hooks are Claude-Code-specific.** Other transform targets silently skip them.

> Note: `mct hook install` (the existing CLI command) installs **git** hooks for save/restore automation. That is unrelated to the `hook` entry type described here.

---

## Skills-only format

For repositories that contain only skills (no agents, no profile hierarchy), `mct` supports a flat structure:

```
skills-repo/
  skills/
    my-skill/
      SKILL.md
    another-skill/
      SKILL.md
```

This is the format used by Anthropic's official skills repository and other community skill collections. `mct` auto-detects this layout when you register a market by scanning for `<name>/SKILL.md` files under a `skills/` or `skills-catalog/` directory.

For skills-only repos, the profile is derived from the skill path itself (e.g. `skills/my-skill`), and the skill's own description is used as the profile description if no README is present.

### Adding a skills-only repo

```bash
# Direct URL — mct auto-detects the skills-only format (registered as "org/skills-repo")
mct market add https://github.com/org/skills-repo.git

# GitHub /tree/ URL — branch and subpath extracted automatically
mct market add https://github.com/org/repo/tree/main/src/skills

# Explicit skills path as a second argument
mct market add https://github.com/org/repo.git src/skills
```

When using a `/tree/` URL, `mct` parses the branch and subdirectory path automatically. The clone is pruned to keep only the skills folder and `.git`, reducing disk usage.

---

## Tool transformations

When a user runs `mct add`, the entry is converted into the native format of every **enabled tool** and written to that tool's expected location. A tool is enabled when `tools.<name>: true` appears in the user's config (`mct config set tools.<name> true`). By default only `claude` is enabled.

### Output paths and capabilities

The full per-tool matrix (skills, agents, commands, hooks and their output paths) is in the [README](./README.md#supported-tools). A tool is only written to when its marker directory exists in the project (e.g. `.codex/`, `.opencode/`). Skills read by several tools go once to the cross-tool `.agents/skills/<name>/` directory, supporting files included; tools that read `.claude/skills/` natively get nothing extra.

### What market authors should know

- **Write entries in Claude Code format.** `mct` converts them; the body is preserved as-is.
- **Skill names** are normalized to the Agent Skills rules (lowercase `a-z0-9-`, max 64 chars) for other tools; keep directory names compliant to avoid surprises.
- **Agents**: `model` (`sonnet`, `opus`, `haiku`, `inherit` or a full id) and `tools` (comma string or list of Claude tool names) are mapped per tool; unmappable values are dropped with a warning. Tools without subagents (pi, Windsurf, Zed, …) skip agents.
- **Commands**: `$ARGUMENTS` is rewritten where the tool uses another placeholder (`{{args}}` for Gemini/Qwen, `$prompt` for Junie). Tools that dropped custom commands receive a user-invocable skill instead.
- **Hooks**: events and matchers are mapped per tool (e.g. `PreToolUse` → `BeforeTool` for Gemini); events with no equivalent are skipped with a warning. OpenCode, Kilo and pi get a generated TypeScript plugin that runs the hook command.

### Tool mappings (`tool-mappings.yml`)

Per-tool defaults for model and tool names live in `mct`. `~/.config/mct/tool-mappings.yml` only holds user overrides; an empty value drops the field:

```yaml
models:
  sonnet:
    opencode: anthropic/claude-sonnet-5-5
    codex: ""
tools:
  Bash:
    gemini: run_shell_command
```

### Per-tool output result

`mct add` reports which tool wrote which file:

```
ok  installed acme@dev/go/agents/reviewer.md
ok  claude    .claude/agents/reviewer.md
ok  codex     .codex/agents/reviewer.toml
ok  opencode  .opencode/agents/reviewer.md
```

In `--json` mode the same information is returned under `tool_writes`.

### Installation as a copy

Skills from any format are installed as **copied directories** into the target tool's location:

```
.claude/
  skills/
    my-skill/
      SKILL.md
      helper.txt
```

All files in the skill directory are copied, so supporting files alongside `SKILL.md` are available locally. Files are copied (not symlinked), keeping your project fully self-contained and independent of the cache directory.

---

## Fields injected by mct

When a user installs an entry, `mct` injects tracking fields into the YAML frontmatter of the **Claude** output (and any other tool whose format preserves frontmatter). **Do not include these in your market files** — `mct` will reject files that already contain them:

| Field | Description | Example |
|-------|-------------|---------|
| `mct_ref` | Full reference | `acme/agents@dev/go/agents/go-developer.md` |
| `mct_version` | Short SHA and date of the last commit that touched the file | `a1b2c3d·2026-03-15` |
| `mct_market` | Market name | `acme/agents` |
| `mct_profile` | Profile path within the market | `dev/go` |
| `mct_installed_at` | Installation timestamp (RFC 3339 UTC) | `2026-03-20T14:30:00Z` |
| `mct_checksum` | Integrity checksum of the original content | `e5f6a7b8...` |

These fields are placed at the top of the frontmatter block, above your original fields, separated by a blank line.

---

## Market naming

When you register a market, `mct` derives the name from the repository URL. The host is stripped, and the remaining path becomes the name:

| URL | Derived name |
|-----|-------------|
| `https://github.com/acme/agents-repo.git` | `acme/agents-repo` |
| `git@gitlab.com:team/skills.git` | `team/skills` |
| `https://github.com/org/mono/tree/main/skills` | `org/mono` |

URL normalization strips protocol prefixes, `git@host:` SSH prefixes, trailing `.git`, and trailing slashes, then lowercases the result. This means the following URLs all resolve to the same identity:

```
https://github.com/Acme/Repo.git
git@github.com:Acme/Repo.git
https://github.com/acme/repo
```

On disk, the clone directory replaces `/` with `--` (e.g. `~/.cache/mct/acme--agents-repo/`).

---

## Linting

Validate your market structure before publishing:

```bash
mct lint [dir]
```

This reports a summary of profiles, agents, and skills found, then lists any issues. Issues are classified as **errors** (exit code 1) or **warnings**:

**Errors:**
- Invalid or missing frontmatter in an entry file
- Cannot determine entry type from the path (not under `agents/` or `skills/`)
- `requires_skills` references a file that doesn't exist in the market

**Warnings:**
- Profile is missing a `README.md`
- Profile README has no `tags` in its frontmatter
- Profile has no agents or skills

Example output:

```
  profiles: 3  agents: 5  skills: 3

  ~  [ops/docker] missing README.md
  ~  [dev/python] README.md has no tags

  0 error(s)  2 warning(s)
```

---

## Publishing

Push your repo to any Git host (GitHub, GitLab, Bitbucket, Gitea, self-hosted). Users register it with:

```bash
# HTTPS (default) — registered as "org/my-market"
mct market add https://github.com/org/my-market.git

# SSH (requires ssh_enabled) — registered as "org/my-market"
mct config set ssh_enabled true
mct market add git@github.com:org/my-market.git
```

The market name is auto-derived from the URL (host is stripped, e.g. `org/my-market`). URLs without a protocol get `https://` prepended automatically, so `github.com/org/my-market` works too.

### Market add options

| Flag | Description |
|------|-------------|
| `--branch <name>` | Track a specific branch (auto-detected if omitted) |
| `--trusted` | Skip breaking change confirmations for this market |
| `--read-only` | Index the market for search but never install from it |
| `--no-clone` | Register the market in config without cloning the repo |
| `--json` | Output result as JSON |

Users can change the tracked branch later:

```bash
mct market set acme/agents branch develop
```

### Market info

Users can inspect a registered market:

```bash
mct market info acme/agents
```

This shows the market name, URL, branch, trusted/read-only/skills-only flags, skills path (if applicable), total entry count, installed count, sync status, and last sync timestamp.

---

## Default markets

When a user runs `mct init` for the first time (or on first invocation when no config exists), `mct` auto-registers a set of community markets from an embedded registry (`assets/skills.json`). Current defaults include Anthropic's official skills repository, `wshobson/agents`, and several popular community collections. Users can remove any of them with `mct market remove`.

---

## Versioning

`mct` uses Git commit SHAs for version tracking — there is no separate version numbering scheme to maintain. Each installed entry records the commit SHA at the time of installation. When `mct sync` runs, it diffs the current HEAD against the recorded SHA and applies only the changes that affect installed files.

The version displayed to users is a compact format: the first 7 characters of the SHA followed by the commit date (e.g. `a1b2c3d·2026-03-15`).

---

## Search indexing

All registered markets are indexed for search. `mct` builds one BM25 document per profile by combining the profile path tokens, README content, README description, tags, and all entry descriptions and filenames. Tags and descriptions are boosted (repeated 3× for term frequency). English stemming (Snowball) is applied to all tokens, and fuzzy matching (Levenshtein distance ≤ 2) expands query terms that have no exact match in the vocabulary.

Search supports filtering by type (`agent` or `skill`), market, category, installed status, and result limit. Deleted entries are excluded by default but can be included with `--include-deleted`.

---

## Conflict and drift policies

Two global configuration settings control how `mct` handles edge cases:

- **`conflict_policy`** (`block` or `skip`): What to do when two markets provide an entry with the same ref. `block` prevents installation; `skip` silently ignores the conflict.
- **`drift_policy`** (`prompt`, `force`, or `skip`): What to do when an installed file has been modified locally and an upstream update arrives. `prompt` asks the user interactively; `force` overwrites; `skip` keeps the local version.

Set them with:

```bash
mct config set conflict_policy block
mct config set drift_policy prompt
```
