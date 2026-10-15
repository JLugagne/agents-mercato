package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestKilo_Basics(t *testing.T) {
	tr := transform.KiloTransformer{}
	require.Equal(t, "kilo", tr.ToolName())
	require.Equal(t, []string{".kilo", ".kilocode", "kilo.json", "kilo.jsonc"}, tr.Detect())
}

func TestKilo_Skill(t *testing.T) {
	in := f1Input(domain.EntryTypeSkill, "dev/skills/Go_Style/SKILL.md", f1Skill)
	in.Assets = []domain.TransformAsset{{Path: "ref/a.md", Content: []byte("A")}}
	res := transform.KiloTransformer{}.Transform(in)
	require.Len(t, res.Files, 2)
	require.Equal(t, ".kilo/skills/go-style/SKILL.md", res.Files[0].Path)
	fm, body := f1Split(t, res.Files[0].Content)
	require.Equal(t, "go-style", fm["name"])
	require.Equal(t, "Go style", fm["description"])
	require.Equal(t, "Use gofmt.\n", body)
	require.Equal(t, ".kilo/skills/go-style/ref/a.md", res.Files[1].Path)
}

func TestKilo_Agent(t *testing.T) {
	res := transform.KiloTransformer{}.Transform(f1Input(domain.EntryTypeAgent, "dev/agents/reviewer.md", f1Agent))
	require.Len(t, res.Files, 1)
	require.Equal(t, ".kilo/agents/reviewer.md", res.Files[0].Path)
	fm, body := f1Split(t, res.Files[0].Content)
	require.Equal(t, "You review.\n", body)
	require.Equal(t, "subagent", fm["mode"])
	require.Equal(t, "anthropic/claude-opus-5-5", fm["model"])
	require.Equal(t, "allow", fm["permission"].(map[string]any)["bash"])
	require.Equal(t, "deny", fm["permission"].(map[string]any)["webfetch"])
	require.Equal(t, []string{"kilo: no equivalent for tools mcp__gh__pr, dropped"}, res.Warnings)
}

func TestKilo_Command(t *testing.T) {
	res := transform.KiloTransformer{}.Transform(f1Input(domain.EntryTypeCommand, "dev/commands/fix.md", f1Command))
	require.Equal(t, ".kilo/commands/fix.md", res.Files[0].Path)
	fm, body := f1Split(t, res.Files[0].Content)
	require.Equal(t, map[string]any{"description": "Fix an issue", "model": "anthropic/claude-haiku-5-5"}, fm)
	require.Equal(t, "Fix $ARGUMENTS then $1.\n", body)
	require.Len(t, res.Warnings, 1)
	require.Contains(t, res.Warnings[0], "positional")

	res = transform.KiloTransformer{}.Transform(f1Input(domain.EntryTypeCommand, "dev/commands/fix.md", "Do $ARGUMENTS\n"))
	require.Empty(t, res.Warnings)
	require.Equal(t, "Do $ARGUMENTS\n", string(res.Files[0].Content))
}

func TestKilo_Hook(t *testing.T) {
	res := transform.KiloTransformer{}.Transform(f1Hook(`{"event":"PreToolUse","matcher":"Bash","hooks":[{"type":"command","command":"guard.sh","timeout":1.5}]}`))
	require.False(t, res.Skipped)
	require.Len(t, res.Files, 1)
	require.Equal(t, ".kilo/plugin/mct-go-vet.ts", res.Files[0].Path)
	ts := string(res.Files[0].Content)
	require.Contains(t, ts, `import type { Plugin } from "@kilocode/plugin"`)
	require.NotContains(t, ts, "@opencode-ai/plugin")
	require.Contains(t, ts, `{ command: "guard.sh", timeout: 1500 },`)
	require.Contains(t, ts, "\nconst MctGoVet: Plugin = async ({ directory }) => ({\n")
	require.NotContains(t, ts, "export const")
	require.Contains(t, ts, `"tool.execute.before": async (input, output) => {`)
	require.Contains(t, ts, `const matcher = new RegExp("^(?:Bash)$")`)
	require.Contains(t, ts, "\nexport default { id: \"mct-go-vet\", server: MctGoVet }\n")

	res = transform.KiloTransformer{}.Transform(f1Hook(`{"event":"Notification","hooks":[{"type":"command","command":"x"}]}`))
	require.True(t, res.Skipped)
}
