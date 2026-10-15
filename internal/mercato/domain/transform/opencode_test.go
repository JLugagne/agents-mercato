package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestOpenCode_Basics(t *testing.T) {
	tr := transform.OpenCodeTransformer{}
	require.Equal(t, "opencode", tr.ToolName())
	require.Equal(t, []string{".opencode", "opencode.json", "opencode.jsonc"}, tr.Detect())
}

func TestOpenCode_SkillIsNative(t *testing.T) {
	res := transform.OpenCodeTransformer{}.Transform(f1Input(domain.EntryTypeSkill, "dev/skills/go/SKILL.md", f1Skill))
	require.Equal(t, domain.TransformResult{ToolName: "opencode"}, res)
}

func TestOpenCode_Agent(t *testing.T) {
	res := transform.OpenCodeTransformer{}.Transform(f1Input(domain.EntryTypeAgent, "dev/agents/reviewer.md", f1Agent))
	require.False(t, res.Skipped)
	require.Len(t, res.Files, 1)
	require.Equal(t, ".opencode/agents/reviewer.md", res.Files[0].Path)
	fm, body := f1Split(t, res.Files[0].Content)
	require.Equal(t, "You review.\n", body)
	require.Equal(t, "Reviews: Go code", fm["description"])
	require.Equal(t, "subagent", fm["mode"])
	require.Equal(t, "anthropic/claude-opus-5-5", fm["model"])
	require.NotContains(t, fm, "tools")
	require.NotContains(t, fm, "name")
	require.Equal(t, map[string]any{
		"read": "allow", "edit": "allow", "bash": "allow", "glob": "deny", "grep": "allow",
		"list": "deny", "webfetch": "deny", "websearch": "deny", "task": "deny", "todowrite": "deny",
	}, fm["permission"])
	require.Equal(t, []string{"opencode: no equivalent for tools mcp__gh__pr, dropped"}, res.Warnings)
}

func TestOpenCode_AgentWithoutToolsOrModel(t *testing.T) {
	in := f1Input(domain.EntryTypeAgent, "dev/agents/plain.md", "---\nmodel: inherit\n---\nBody\n")
	in.Entry.Description = "From index"
	res := transform.OpenCodeTransformer{}.Transform(in)
	fm, _ := f1Split(t, res.Files[0].Content)
	require.Equal(t, map[string]any{"description": "From index", "mode": "subagent"}, fm)
	require.Empty(t, res.Warnings)
}

func TestOpenCode_AgentModels(t *testing.T) {
	cases := map[string]string{
		"sonnet":                     "anthropic/claude-sonnet-5-5",
		"haiku":                      "anthropic/claude-haiku-5-5",
		"claude-sonnet-4-5-20250929": "anthropic/claude-sonnet-4-5-20250929",
	}
	for model, want := range cases {
		res := transform.OpenCodeTransformer{}.Transform(f1Input(domain.EntryTypeAgent, "agents/a.md", "---\ndescription: d\nmodel: "+model+"\n---\nB\n"))
		fm, _ := f1Split(t, res.Files[0].Content)
		require.Equal(t, want, fm["model"], model)
	}

	res := transform.OpenCodeTransformer{}.Transform(f1Input(domain.EntryTypeAgent, "agents/a.md", "---\ndescription: d\nmodel: gpt-5\n---\nB\n"))
	fm, _ := f1Split(t, res.Files[0].Content)
	require.NotContains(t, fm, "model")
	require.Len(t, res.Warnings, 1)

	in := f1Input(domain.EntryTypeAgent, "agents/a.md", "---\ndescription: d\nmodel: opus\ntools: Bash\n---\nB\n")
	in.Mappings = domain.ToolMapping{
		Models: map[string]map[string]string{"opus": {"opencode": "openrouter/opus"}},
		Tools:  map[string]map[string]string{"Bash": {"opencode": "skill"}},
	}
	res = transform.OpenCodeTransformer{}.Transform(in)
	fm, _ = f1Split(t, res.Files[0].Content)
	require.Equal(t, "openrouter/opus", fm["model"])
	perm := fm["permission"].(map[string]any)
	require.Equal(t, "deny", perm["bash"])
	require.Equal(t, "allow", perm["skill"])
}

func TestOpenCode_Command(t *testing.T) {
	res := transform.OpenCodeTransformer{}.Transform(f1Input(domain.EntryTypeCommand, "dev/commands/fix.md", f1Command))
	require.Len(t, res.Files, 1)
	require.Equal(t, ".opencode/commands/fix.md", res.Files[0].Path)
	fm, body := f1Split(t, res.Files[0].Content)
	require.Equal(t, map[string]any{"description": "Fix an issue", "model": "anthropic/claude-haiku-5-5"}, fm)
	require.Equal(t, "Fix $ARGUMENTS then $1.\n", body)
	require.Empty(t, res.Warnings)
}

func TestOpenCode_HookPreToolUse(t *testing.T) {
	res := transform.OpenCodeTransformer{}.Transform(f1Hook(`{"event":"PreToolUse","matcher":"Edit|Write","hooks":[{"type":"command","command":"echo \"x\" </dev/null","timeout":30},{"type":"command","command":"true"}]}`))
	require.False(t, res.Skipped)
	require.Empty(t, res.Warnings)
	require.Empty(t, res.Hooks)
	require.Len(t, res.Files, 1)
	require.Equal(t, ".opencode/plugins/mct-go-vet.ts", res.Files[0].Path)
	ts := string(res.Files[0].Content)
	require.Contains(t, ts, `import type { Plugin } from "@opencode-ai/plugin"`+"\n")
	require.Contains(t, ts, `import { spawnSync } from "node:child_process"`)
	require.Contains(t, ts, `{ command: "echo \"x\" </dev/null", timeout: 30000 },`)
	require.Contains(t, ts, `{ command: "true" },`)
	require.Contains(t, ts, `const matcher = new RegExp("^(?:Edit|Write)$")`)
	require.Contains(t, ts, `"apply_patch": "Edit",`)
	require.Contains(t, ts, `"list": "LS",`)
	require.Contains(t, ts, `spawnSync("sh", ["-c", handler.command], {`)
	require.Contains(t, ts, "export const MctGoVet: Plugin = async ({ directory }) => ({\n")
	require.Contains(t, ts, `"tool.execute.before": async (input, output) => {`)
	require.Contains(t, ts, `const toolName = claudeToolName(field(input, "tool"))`)
	require.Contains(t, ts, "if (!matcher.test(toolName)) return")
	require.Contains(t, ts, `hook_event_name: "PreToolUse",`)
	require.Contains(t, ts, `tool_input: field(output, "args"),`)
	require.Contains(t, ts, "if (blocked !== undefined) throw new Error(blocked)")
	require.NotContains(t, ts, "export default")
	require.True(t, ts[len(ts)-3:] == "})\n")

	again := transform.OpenCodeTransformer{}.Transform(f1Hook(`{"event":"PreToolUse","matcher":"Edit|Write","hooks":[{"type":"command","command":"echo \"x\" </dev/null","timeout":30},{"type":"command","command":"true"}]}`))
	require.Equal(t, res.Files[0].Content, again.Files[0].Content)
}

func TestOpenCode_HookEvents(t *testing.T) {
	cases := []struct {
		event, want, notWant string
	}{
		{"PostToolUse", `"tool.execute.after": async (input, output) => {`, "throw new Error"},
		{"SessionStart", `if (field(event, "type") !== "session.created") return`, "claudeToolName"},
		{"Stop", `if (field(event, "type") !== "session.idle") return`, "matcher"},
		{"PreCompact", `"experimental.session.compacting": async (input) => {`, "toolNames"},
		{"UserPromptSubmit", `prompt: promptText(field(output, "parts")),`, "throw new Error"},
	}
	for _, c := range cases {
		res := transform.OpenCodeTransformer{}.Transform(f1Hook(`{"event":"` + c.event + `","hooks":[{"type":"command","command":"x"}]}`))
		require.False(t, res.Skipped, c.event)
		ts := string(res.Files[0].Content)
		require.Contains(t, ts, c.want, c.event)
		require.Contains(t, ts, `hook_event_name: "`+c.event+`",`, c.event)
		require.NotContains(t, ts, c.notWant, c.event)
		require.NotContains(t, ts, "new RegExp", c.event)
		if c.event == "UserPromptSubmit" {
			require.Len(t, res.Warnings, 1)
		} else {
			require.Empty(t, res.Warnings, c.event)
		}
	}
}

func TestOpenCode_HookMatcherHandling(t *testing.T) {
	res := transform.OpenCodeTransformer{}.Transform(f1Hook(`{"event":"PostToolUse","matcher":"*","hooks":[{"type":"command","command":"x"}]}`))
	ts := string(res.Files[0].Content)
	require.NotContains(t, ts, "matcher")
	require.Contains(t, ts, "const toolName = claudeToolName")

	res = transform.OpenCodeTransformer{}.Transform(f1Hook(`{"event":"SessionStart","matcher":"startup","hooks":[{"type":"command","command":"x"}]}`))
	require.Equal(t, []string{`opencode: hook matcher "startup" ignored for event SessionStart`}, res.Warnings)
}

func TestOpenCode_HookUnsupported(t *testing.T) {
	for _, ev := range []string{"SubagentStop", "SessionEnd", "Notification"} {
		res := transform.OpenCodeTransformer{}.Transform(f1Hook(`{"event":"` + ev + `","hooks":[{"type":"command","command":"x"}]}`))
		require.True(t, res.Skipped, ev)
		require.Contains(t, res.SkipReason, ev)
		require.Empty(t, res.Files)
	}

	res := transform.OpenCodeTransformer{}.Transform(f1Hook(`{"event":"Stop","hooks":[{"type":"prompt","prompt":"x"}]}`))
	require.True(t, res.Skipped)
	require.Contains(t, res.SkipReason, "invalid hook")
}
