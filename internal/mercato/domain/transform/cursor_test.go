package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestCursor(t *testing.T) {
	tr := transform.CursorTransformer{}
	require.Equal(t, "cursor", tr.ToolName())
	require.Equal(t, []string{".cursor", ".cursorrules"}, tr.Detect())

	t.Run("native skill and agent", func(t *testing.T) {
		for _, typ := range []domain.EntryType{domain.EntryTypeSkill, domain.EntryTypeAgent} {
			res := tr.Transform(f2Input(typ, "d/x/y.md", f2Skill))
			require.False(t, res.Skipped)
			require.Equal(t, "cursor", res.ToolName)
			require.Empty(t, res.Files)
			require.Empty(t, res.Warnings)
		}
	})

	t.Run("command becomes skill", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeCommand, "d/commands/fix.md", f2Command))
		require.Equal(t, ".cursor/skills/fix/SKILL.md", res.Files[0].Path)
		fm := f2Frontmatter(t, res.Files[0].Content)
		require.Equal(t, "fix", fm["name"])
		require.Equal(t, true, fm["disable-model-invocation"])
	})

	t.Run("hook", func(t *testing.T) {
		res := tr.Transform(f2Hook("UserPromptSubmit", "Bash", "12"))
		require.Len(t, res.Hooks, 1)
		p := res.Hooks[0]
		require.Equal(t, ".cursor/hooks.json", p.File)
		require.Equal(t, []string{"hooks"}, p.Keys)
		require.Equal(t, "beforeSubmitPrompt", p.Event)
		require.Equal(t, map[string]any{"version": 1}, p.Header)
		require.JSONEq(t, `{"type":"command","command":"go vet ./...","timeout":12,"matcher":"Bash"}`, string(p.Entry))
	})

	t.Run("one patch per handler", func(t *testing.T) {
		in := f2Input(domain.EntryTypeHook, "d/hooks/h.json", `{"event":"Stop","hooks":[{"type":"command","command":"a"},{"type":"command","command":"b"},{"type":"prompt"}]}`)
		res := tr.Transform(in)
		require.Len(t, res.Hooks, 2)
		require.Equal(t, "stop", res.Hooks[1].Event)
		require.JSONEq(t, `{"type":"command","command":"b"}`, string(res.Hooks[1].Entry))
		require.NotEmpty(t, res.Warnings)
	})

	t.Run("notification has no equivalent", func(t *testing.T) {
		res := tr.Transform(f2Hook("Notification", "", ""))
		require.True(t, res.Skipped)
		require.Empty(t, res.Hooks)
	})
}
