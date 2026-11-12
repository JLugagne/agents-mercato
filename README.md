# mct — a package manager for AI agents, skills, commands and hooks

`mct` installs agents, skills, slash commands and Claude Code hooks from Git repositories ("markets") into your projects or your home directory, keeps them up to date, and converts them for the other AI tools you use (Codex, Cursor, Copilot, Gemini, OpenCode, pi, Kilo, Factory, Qwen, Kiro, Windsurf…).

No central registry, no server. Just Git.

<p align="center">
  <img src="docs/screenshot-tui.png" width="700" alt="mct TUI" />
</p>

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/JLugagne/agents-mercato/main/install.sh | bash
```

This installs `~/.local/bin/mct` without root. Alternatives: `go install github.com/JLugagne/agents-mercato/cmd/mct@latest`, and `mct upgrade` to update later (the download is checksum-verified).

## How it works in 30 seconds

```bash
mct market add team git@github.com:my-org/agents.git   # register a source
mct search "code review"                               # find something
mct add team@dev/go/agents/reviewer.md                 # install it
```

A ref is `market@path`: the market name you registered, then the file path inside the repo. Files are **copied** into your project (`.claude/agents/`, `.claude/skills/`…), so each project owns its copy and `mct` tracks whether you edited it.

## Use cases

### I want a skill or agent in my current project

```bash
mct add team@dev/go/skills/testing/SKILL.md
mct add team@dev/go/skills/testing/SKILL.md --dry-run   # preview first
```

Skills the entry depends on are installed with it. If a dependency lives in another market, `mct` offers to register it.

### I want it available in every project (my personal setup)

```bash
mct add team@dev/go/skills/testing/SKILL.md --global
```

`--global` (`-g`) installs into `~/.claude` and `~/.agents` instead of the current project. To keep these up to date, run `mct sync` from your home directory (`cd ~ && mct sync`).

### I use several AI tools, not only Claude Code

By default, `mct` writes for Claude Code (`.claude/`) and to the shared `.agents/skills/` directory read by many other tools. Turn on more tools and every install is converted for them:

```bash
mct config set tools.codex true
mct config set tools.opencode true
mct add team@dev/go/agents/reviewer.md
# .claude/agents/reviewer.md
# .codex/agents/reviewer.toml
# .opencode/agents/reviewer.md
```

A tool is only written to when it is present in the project (for example a `.codex/` directory exists). To target a tool for a single install: `mct add <ref> --tools codex`. The full list of supported tools and their formats is in [MARKET.md](./MARKET.md).

### I edited an installed skill and want the upstream version back

```bash
mct add team@dev/go/skills/testing/SKILL.md --force
```

`--force` (`-f`) reinstalls an entry that is already installed and overwrites your local changes.

### I want to know what changed upstream

```bash
mct check      # status of everything installed: clean, update available, modified locally…
mct sync       # fetch, then apply updates
mct doctor     # offline audit: modified, deleted or orphaned files, in one report
```

`mct sync` never overwrites a file you modified without asking. If you deleted an installed file, it offers to restore it. In CI, use `mct sync --restore-none` to skip that prompt.

### I want the whole team (and CI) to get the same setup

On your machine:

```bash
mct save                    # writes .mct.json
git add .mct.json && git commit -m "add mct setup" && git push
```

On a teammate's machine or in CI:

```bash
git pull
mct restore                 # reinstalls everything listed in .mct.json
```

To do it automatically on `git pull` and `git push`:

```bash
mct hook install post-pull
mct hook install pre-push
```

GitHub Actions example:

```yaml
- name: Restore agents and skills
  run: mct restore
```

### I want to remove something

```bash
mct remove team@dev/go/skills/testing/SKILL.md
mct prune        # handle entries that were deleted upstream
```

### I want to publish my own agents and skills

Create a Git repository with this layout and register it with `mct market add`:

```
my-market/
  dev/go/
    agents/reviewer.md
    skills/testing/SKILL.md
    commands/release.md
    hooks/format.json
```

Check it before you publish with `mct lint <market>`. The full spec is in [MARKET.md](./MARKET.md).

### I prefer browsing to typing

```bash
mct tui
```

## Good to know

- **Private repositories**: SSH URLs work with your SSH agent and `~/.ssh/config`.
- **Scripting**: most commands accept `--json`; use `--ci` to disable interactive prompts.
- **Old installs**: files written by older `mct` versions are converted automatically by `mct update` / `mct sync`, or explicitly with `mct migrate --dry-run`.
- **Safety**: installs, updates and removals are atomic; a crash never leaves a half-written tree.
- **Everything else**: `mct --help` and `mct <command> --help`.

## Contributing

Issues and pull requests are welcome. Each supported tool is a small, self-contained transformer in `internal/mercato/domain/transform/`, so adding a tool is a good first contribution.

## License

[MIT](LICENSE)
