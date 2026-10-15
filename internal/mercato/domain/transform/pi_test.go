package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestPi_Basics(t *testing.T) {
	tr := transform.PiTransformer{}
	require.Equal(t, "pi", tr.ToolName())
	require.Equal(t, []string{".pi"}, tr.Detect())
}

func TestPi_SkillShared(t *testing.T) {
	res := transform.PiTransformer{}.Transform(f1Input(domain.EntryTypeSkill, "dev/skills/go/SKILL.md", f1Skill))
	require.Len(t, res.Files, 1)
	require.Equal(t, ".agents/skills/go/SKILL.md", res.Files[0].Path)
	require.True(t, res.Files[0].IsShared())
	fm, _ := f1Split(t, res.Files[0].Content)
	require.Equal(t, "go", fm["name"])
}

func TestPi_AgentUnsupported(t *testing.T) {
	res := transform.PiTransformer{}.Transform(f1Input(domain.EntryTypeAgent, "dev/agents/reviewer.md", f1Agent))
	require.True(t, res.Skipped)
	require.Equal(t, "pi", res.ToolName)
	require.Empty(t, res.Files)
}

func TestPi_Command(t *testing.T) {
	res := transform.PiTransformer{}.Transform(f1Input(domain.EntryTypeCommand, "dev/commands/fix.md", f1Command))
	require.Len(t, res.Files, 1)
	require.Equal(t, ".pi/prompts/fix.md", res.Files[0].Path)
	fm, body := f1Split(t, res.Files[0].Content)
	require.Equal(t, map[string]any{"description": "Fix an issue", "argument-hint": "<issue>"}, fm)
	require.Equal(t, "Fix $ARGUMENTS then $1.\n", body)
	require.Empty(t, res.Warnings)
}

func TestPi_HookPreToolUse(t *testing.T) {
	res := transform.PiTransformer{}.Transform(f1Hook(`{"event":"PreToolUse","matcher":"Glob|LS","hooks":[{"type":"command","command":"check","timeout":10}]}`))
	require.False(t, res.Skipped)
	require.Empty(t, res.Warnings)
	require.Len(t, res.Files, 1)
	require.Equal(t, ".pi/extensions/mct-go-vet.ts", res.Files[0].Path)
	ts := string(res.Files[0].Content)
	require.Contains(t, ts, `import type { ExtensionAPI } from "@earendil-works/pi-coding-agent"`)
	require.Contains(t, ts, "export default function (pi: ExtensionAPI) {\n")
	require.Contains(t, ts, `pi.on("tool_call", async (event) => {`)
	require.Contains(t, ts, `const toolName = claudeToolName(field(event, "toolName"))`)
	require.Contains(t, ts, `const matcher = new RegExp("^(?:Glob|LS)$")`)
	require.Contains(t, ts, `"find": "Glob",`)
	require.Contains(t, ts, `"ls": "LS",`)
	require.Contains(t, ts, `{ command: "check", timeout: 10000 },`)
	require.Contains(t, ts, "runHandlers(process.cwd(), {")
	require.Contains(t, ts, `tool_input: field(event, "input"),`)
	require.Contains(t, ts, "if (blocked !== undefined) return { block: true, reason: blocked }")
	require.NotContains(t, ts, "throw new Error")
}

func TestPi_HookEvents(t *testing.T) {
	cases := map[string]string{
		"PostToolUse":      `pi.on("tool_result", async (event) => {`,
		"SessionStart":     `pi.on("session_start", async (event) => {`,
		"SessionEnd":       `pi.on("session_shutdown", async (event) => {`,
		"Stop":             `pi.on("agent_end", async (event) => {`,
		"UserPromptSubmit": `pi.on("input", async (event) => {`,
	}
	for ev, want := range cases {
		res := transform.PiTransformer{}.Transform(f1Hook(`{"event":"` + ev + `","hooks":[{"type":"command","command":"x"}]}`))
		require.False(t, res.Skipped, ev)
		require.Empty(t, res.Warnings, ev)
		ts := string(res.Files[0].Content)
		require.Contains(t, ts, want, ev)
		require.Contains(t, ts, `hook_event_name: "`+ev+`",`, ev)
		require.NotContains(t, ts, "block: true", ev)
	}
	ts := string(transform.PiTransformer{}.Transform(f1Hook(`{"event":"UserPromptSubmit","hooks":[{"type":"command","command":"x"}]}`)).Files[0].Content)
	require.Contains(t, ts, `prompt: field(event, "text"),`)
}

func TestPi_HookUnsupported(t *testing.T) {
	for _, ev := range []string{"PreCompact", "SubagentStop", "Notification"} {
		res := transform.PiTransformer{}.Transform(f1Hook(`{"event":"` + ev + `","hooks":[{"type":"command","command":"x"}]}`))
		require.True(t, res.Skipped, ev)
		require.Contains(t, res.SkipReason, ev)
	}
}
