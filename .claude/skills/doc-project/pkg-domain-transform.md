# domain/transform — multi-tool targets

Converts mct entries (always authored in Claude Code format) into the
project-level format of other AI coding tools. Pure code: no I/O, everything
comes through `domain.TransformInput`. Formats were verified against each
tool's docs on 2026-10-15.

## Contract (`domain/transformer.go`)

```go
type Transformer interface {
    ToolName() string
    Detect() []string                        // project-relative markers; tool skipped when none exists
    Transform(in TransformInput) TransformResult
}
TransformInput{Entry, Content, Assets []TransformAsset, Mappings ToolMapping, Exists func(rel) bool}
TransformResult{ToolName, Files []OutputFile, Hooks []HookPatch, Warnings, Skipped, SkipReason}
OutputFile{Path, Content}                    // Path under ".agents/" = shared (IsShared)
HookPatch{File, Keys, Event, Entry, Header}  // append Entry to File#Keys/Event (JSON shared with the user)
```

- `Assets` = supporting files of a directory skill (paths relative to the skill dir).
- "native" = the tool already reads `.claude/<x>`, the transformer returns no file.
- `Registry()` (`registry.go`) returns every tool keyed by name; `init.go` wires it.
- Defaults for model / tool-name mapping live in each transformer
  (`map[string]string` tables); `tool-mappings.yml` only holds user overrides
  (`mapModel`, `mapTools`, `mapMatcher` in `common.go`: user mapping > default;
  empty value = drop).

## Helpers (`common.go`)

`parseDoc` (yaml.Node frontmatter, `str`, `list` accepts comma strings or
lists), `renderMarkdown` (ordered, YAML-safe), `skillFiles` / `sharedSkillFiles`
(forces Agent Skills `name` = dir name, copies assets), `commandSkillFiles`
(command → user-invocable skill), `parseHook` (Claude snippet → handlers),
`matcherTools`, `replaceArguments`, `tomlString`, `mustJSON`. TypeScript hook
shims: `tsshim.go`; JSON hook helpers: `jsonhooks.go`; workflow helpers:
`workflow_common.go`.

## Tool matrix

| Tool | Detect | Skills | Agents | Commands | Hooks |
|---|---|---|---|---|---|
| claude | — | core path | core path | core path | core path |
| opencode | `.opencode`, `opencode.json(c)` | native | `.opencode/agents/*.md` (mode subagent, permission map) | `.opencode/commands/*.md` | TS plugin `.opencode/plugins/mct-*.ts` (v1 API) |
| kilo | `.kilo`, `.kilocode`, `kilo.json(c)` | `.kilo/skills` | `.kilo/agents/*.md` | `.kilo/commands/*.md` | TS plugin `.kilo/plugin/mct-*.ts` |
| pi | `.pi` | shared | — | `.pi/prompts/*.md` | TS extension `.pi/extensions/mct-*.ts` |
| codex | `.codex` | shared | `.codex/agents/*.toml` | — (deprecated upstream) | patch `.codex/hooks.json#hooks` |
| gemini | `.gemini`, `GEMINI.md` | shared | `.gemini/agents/*.md` | `.gemini/commands/*.toml` (`{{args}}`) | patch `.gemini/settings.json#hooks` (BeforeTool…, ms) |
| qwen | `.qwen`, `QWEN.md` | `.qwen/skills` | `.qwen/agents/*.md` | `.qwen/commands/*.md` (`{{args}}`) | patch `.qwen/settings.json#hooks` |
| factory | `.factory` | shared | `.factory/droids/*.md` | `.factory/commands/*.md` | patch `.factory/hooks.json` (root) |
| cursor | `.cursor`, `.cursorrules` | native | native | skill `.cursor/skills/*` | patch `.cursor/hooks.json#hooks` (v1, camelCase) |
| copilot | `.github/{copilot-instructions.md,agents,skills,prompts,instructions,hooks}` | native | native | skill `.github/skills/*` | file `.github/hooks/mct-*.json` |
| windsurf | `.windsurf`, `.devin`, `.windsurfrules` | shared | — | `<.devin or .windsurf>/workflows/*.md` | patch `<base>/hooks.json#hooks` (pre_run_command…) |
| kiro | `.kiro` | `.kiro/skills` | `.kiro/agents/*.md` | skill `.kiro/skills/*` | file `.kiro/hooks/mct-*.json` (v1) |
| cline | `.clinerules`, `.cline` | native | — | `.clinerules/workflows/*.md` | — (script contract differs) |
| augment | `.augment` | native | `.augment/agents/*.md` | native | — (needs .sh scripts) |
| junie | `.junie` | shared | `.junie/agents/*.md` | `.junie/commands/*.md` (`$prompt`) | — (project hooks ignored) |
| vibe | `.vibe` | shared | `.vibe/agents/*.toml` | skill `.vibe/skills/*` | — (TOML shared file) |
| antigravity | `.agents`, `.agent` | shared | — | skill `.agents/skills/*` | patch `.agents/hooks.json#mct-<name>` |
| zed | `.zed` | shared | — | — | — |
| trae | `.trae` | `.trae/skills` | — | — | — |
| deepcode (DeepSeek) | `.deepcode` | `.deepcode/skills` | — | skill `.deepcode/skills/*` | — |
| codewhale (ex DeepSeek-TUI) | `.codewhale` | native | `.codewhale/agents/*.toml` | — | — |
| roocode (legacy) | `.roo`, `.roorules` | `.roo/rules/*.md` | — | — | — |
| continue (legacy) | `.continue` | `.continue/rules/*.md` | — | — | — |

"shared" = `.agents/skills/<name>/` (read by pi, codex, gemini, factory,
windsurf/devin, junie, vibe, antigravity, zed, and natively by many others).
Amp, Crush and Goose read `.claude/skills` natively and need no target.
Supermaven and PearAI were removed (dead products); their recorded files are
deleted on the next update.

## TS hook shims

One file per Claude hook. The shim maps the tool's tool ids to Claude names,
tests the Claude matcher as `^(?:matcher)$`, and runs each handler with
`spawnSync("sh", ["-c", cmd], {input: <Claude-style JSON payload>})`. Exit code
2 on PreToolUse blocks (opencode/kilo: `throw new Error(stderr)`; pi:
`{block: true, reason}`).
