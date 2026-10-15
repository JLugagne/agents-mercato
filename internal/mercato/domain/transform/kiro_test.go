package transform_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestKiro_SkillAndCommand(t *testing.T) {
	tr := transform.KiroTransformer{}
	require.Equal(t, "kiro", tr.ToolName())
	require.Equal(t, []string{".kiro"}, tr.Detect())
	r := tr.Transform(f3Input(domain.EntryTypeSkill, "skills/go/SKILL.md", "---\ndescription: d\n---\nb"))
	require.Equal(t, ".kiro/skills/go/SKILL.md", r.Files[0].Path)
	r = tr.Transform(f3Input(domain.EntryTypeCommand, "commands/run.md", "---\ndescription: Run\n---\nGo $ARGUMENTS"))
	require.Equal(t, ".kiro/skills/run/SKILL.md", r.Files[0].Path)
	require.Contains(t, string(r.Files[0].Content), "$ARGUMENTS")
}

func TestKiro_Agent(t *testing.T) {
	in := f3Input(domain.EntryTypeAgent, "agents/rev.md", "---\nname: rev\ndescription: Reviews\nmodel: opus\ntools: Read, Grep, Edit, Bash, Weird\n---\nPrompt\n")
	r := transform.KiroTransformer{}.Transform(in)
	require.Equal(t, ".kiro/agents/rev.md", r.Files[0].Path)
	fm := f3Frontmatter(t, r.Files[0].Content)
	require.Equal(t, "rev", fm["name"])
	require.Equal(t, "Reviews", fm["description"])
	require.Equal(t, []any{"read", "write", "shell"}, fm["tools"])
	require.NotContains(t, fm, "model")
	require.Contains(t, string(r.Files[0].Content), "Prompt\n")
	require.Len(t, r.Warnings, 1)
}

func TestKiro_Hook(t *testing.T) {
	tr := transform.KiroTransformer{}
	r := tr.Transform(f3Input(domain.EntryTypeHook, "hooks/lint.json", f3Hook("PreToolUse", "Write|Edit|Bash", 10)))
	require.Equal(t, ".kiro/hooks/mct-lint.json", r.Files[0].Path)
	var got map[string]any
	require.NoError(t, json.Unmarshal(r.Files[0].Content, &got))
	require.Equal(t, "v1", got["version"])
	item := got["hooks"].([]any)[0].(map[string]any)
	require.Equal(t, "mct-lint", item["name"])
	require.Equal(t, "PreToolUse", item["trigger"])
	require.Equal(t, "write|shell", item["matcher"])
	require.Equal(t, float64(10), item["timeout"])
	require.Equal(t, true, item["enabled"])
	require.Equal(t, map[string]any{"type": "command", "command": "echo hi"}, item["action"])

	r = tr.Transform(f3Input(domain.EntryTypeHook, "hooks/lint.json", f3Hook("Stop", "", 0)))
	require.NoError(t, json.Unmarshal(r.Files[0].Content, &got))
	item = got["hooks"].([]any)[0].(map[string]any)
	require.NotContains(t, item, "matcher")
	require.NotContains(t, item, "timeout")

	require.True(t, tr.Transform(f3Input(domain.EntryTypeHook, "hooks/x.json", f3Hook("PreCompact", "", 0))).Skipped)
}
