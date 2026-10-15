package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestQwen(t *testing.T) {
	tr := transform.QwenTransformer{}
	require.Equal(t, "qwen", tr.ToolName())
	require.Equal(t, []string{".qwen", "QWEN.md"}, tr.Detect())

	t.Run("skill", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeSkill, "d/skills/go-x/SKILL.md", f2Skill))
		require.Equal(t, ".qwen/skills/go-x/SKILL.md", res.Files[0].Path)
	})

	t.Run("agent", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeAgent, "d/agents/reviewer.md", f2Agent))
		require.Equal(t, ".qwen/agents/reviewer.md", res.Files[0].Path)
		fm := f2Frontmatter(t, res.Files[0].Content)
		require.Equal(t, "code-reviewer", fm["name"])
		require.Equal(t, []any{"read_file", "run_shell_command", "edit"}, fm["tools"])
		require.NotContains(t, fm, "model")
		require.Len(t, res.Warnings, 2)
	})

	t.Run("command", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeCommand, "d/commands/fix.md", f2Command))
		require.Equal(t, ".qwen/commands/fix.md", res.Files[0].Path)
		require.Equal(t, "Fix it", f2Frontmatter(t, res.Files[0].Content)["description"])
		require.Contains(t, string(res.Files[0].Content), "Fix {{args}} now, then $1.")
		require.Len(t, res.Warnings, 1)
	})

	t.Run("hook", func(t *testing.T) {
		res := tr.Transform(f2Hook("PostToolUse", "Edit|WebSearch", "7"))
		p := res.Hooks[0]
		require.Equal(t, ".qwen/settings.json", p.File)
		require.Equal(t, []string{"hooks"}, p.Keys)
		require.Equal(t, "PostToolUse", p.Event)
		require.JSONEq(t, `{"matcher":"edit|web_search","hooks":[{"type":"command","command":"go vet ./...","timeout":7}]}`, string(p.Entry))
	})

	t.Run("hook without matcher", func(t *testing.T) {
		res := tr.Transform(f2Hook("Notification", "", ""))
		require.Equal(t, `{"hooks":[{"command":"go vet ./...","type":"command"}]}`, string(res.Hooks[0].Entry))
	})
}
