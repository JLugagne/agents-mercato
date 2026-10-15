package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestCodex(t *testing.T) {
	tr := transform.CodexTransformer{}
	require.Equal(t, "codex", tr.ToolName())
	require.Equal(t, []string{".codex"}, tr.Detect())

	t.Run("skill", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeSkill, "d/skills/go-x/SKILL.md", f2Skill))
		require.Len(t, res.Files, 1)
		require.Equal(t, ".agents/skills/go-x/SKILL.md", res.Files[0].Path)
	})

	t.Run("agent", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeAgent, "d/agents/reviewer.md", f2Agent))
		require.Len(t, res.Files, 1)
		require.Equal(t, ".codex/agents/reviewer.toml", res.Files[0].Path)
		s := string(res.Files[0].Content)
		require.Contains(t, s, `name = "code-reviewer"`)
		require.Contains(t, s, `description = "Reviews code"`)
		require.Contains(t, s, `developer_instructions = "You review.\n"`)
		require.NotContains(t, s, "model")
		require.Len(t, res.Warnings, 2)
	})

	t.Run("agent inherit has no warning", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeAgent, "d/agents/a.md", "---\ndescription: d\nmodel: inherit\n---\nB"))
		require.Empty(t, res.Warnings)
	})

	t.Run("command skipped", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeCommand, "d/commands/c.md", f2Command))
		require.True(t, res.Skipped)
		require.Contains(t, res.SkipReason, "deprecated")
	})

	t.Run("hook", func(t *testing.T) {
		res := tr.Transform(f2Hook("PreToolUse", "Bash", "30"))
		require.False(t, res.Skipped)
		require.Len(t, res.Hooks, 1)
		p := res.Hooks[0]
		require.Equal(t, ".codex/hooks.json", p.File)
		require.Equal(t, []string{"hooks"}, p.Keys)
		require.Equal(t, "PreToolUse", p.Event)
		require.JSONEq(t, `{"matcher":"Bash","hooks":[{"type":"command","command":"go vet ./...","timeout":30}]}`, string(p.Entry))
	})

	t.Run("hook without matcher or timeout", func(t *testing.T) {
		res := tr.Transform(f2Hook("Stop", "", ""))
		require.Equal(t, `{"hooks":[{"command":"go vet ./...","type":"command"}]}`, string(res.Hooks[0].Entry))
	})

	t.Run("notification skipped", func(t *testing.T) {
		res := tr.Transform(f2Hook("Notification", "", ""))
		require.True(t, res.Skipped)
		require.NotEmpty(t, res.Warnings)
		require.Empty(t, res.Hooks)
	})
}
