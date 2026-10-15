package transform_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestCopilot(t *testing.T) {
	tr := transform.CopilotTransformer{}
	require.Equal(t, "copilot", tr.ToolName())
	require.Contains(t, tr.Detect(), ".github/copilot-instructions.md")
	require.Len(t, tr.Detect(), 6)

	t.Run("native skill and agent", func(t *testing.T) {
		for _, typ := range []domain.EntryType{domain.EntryTypeSkill, domain.EntryTypeAgent} {
			res := tr.Transform(f2Input(typ, "d/x/y.md", f2Skill))
			require.False(t, res.Skipped)
			require.Empty(t, res.Files)
			require.Empty(t, res.Warnings)
		}
	})

	t.Run("command becomes skill", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeCommand, "d/commands/fix.md", f2Command))
		require.Equal(t, ".github/skills/fix/SKILL.md", res.Files[0].Path)
		require.Equal(t, true, f2Frontmatter(t, res.Files[0].Content)["disable-model-invocation"])
	})

	t.Run("hook", func(t *testing.T) {
		res := tr.Transform(f2Hook("UserPromptSubmit", "", "20"))
		require.Len(t, res.Files, 1)
		require.Empty(t, res.Hooks)
		require.Empty(t, res.Warnings)
		require.Equal(t, ".github/hooks/mct-lint-hook.json", res.Files[0].Path)
		content := string(res.Files[0].Content)
		require.Equal(t, byte('\n'), content[len(content)-1])
		require.Contains(t, content, "\n  \"hooks\"")
		var got map[string]any
		require.NoError(t, json.Unmarshal(res.Files[0].Content, &got))
		require.Equal(t, map[string]any{
			"version": float64(1),
			"hooks": map[string]any{"userPromptSubmitted": []any{
				map[string]any{"type": "command", "bash": "go vet ./...", "timeoutSec": float64(20)},
			}},
		}, got)
	})

	t.Run("matcher warns", func(t *testing.T) {
		res := tr.Transform(f2Hook("PreToolUse", "Bash", ""))
		require.Equal(t, []string{"copilot hooks have no matcher, the hook runs for every tool"}, res.Warnings)
		require.Contains(t, string(res.Files[0].Content), "preToolUse")
		require.NotContains(t, string(res.Files[0].Content), "timeoutSec")
	})

	t.Run("unsupported events", func(t *testing.T) {
		for _, ev := range []string{"PreCompact", "Notification"} {
			res := tr.Transform(f2Hook(ev, "", ""))
			require.True(t, res.Skipped)
			require.Empty(t, res.Files)
		}
	})
}
