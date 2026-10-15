package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestGemini(t *testing.T) {
	tr := transform.GeminiTransformer{}
	require.Equal(t, "gemini", tr.ToolName())
	require.Equal(t, []string{".gemini", "GEMINI.md"}, tr.Detect())

	t.Run("skill", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeSkill, "d/skills/go-x/SKILL.md", f2Skill))
		require.Equal(t, ".agents/skills/go-x/SKILL.md", res.Files[0].Path)
	})

	t.Run("agent", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeAgent, "d/agents/reviewer.md", f2Agent))
		require.Equal(t, ".gemini/agents/reviewer.md", res.Files[0].Path)
		fm := f2Frontmatter(t, res.Files[0].Content)
		require.Equal(t, "code-reviewer", fm["name"])
		require.Equal(t, "Reviews code", fm["description"])
		require.Equal(t, []any{"read_file", "run_shell_command", "replace"}, fm["tools"])
		require.NotContains(t, fm, "model")
		require.Len(t, res.Warnings, 2)
		require.Contains(t, string(res.Files[0].Content), "You review.")
	})

	t.Run("agent without tools", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeAgent, "d/agents/a.md", "---\ndescription: d\n---\nB"))
		require.NotContains(t, f2Frontmatter(t, res.Files[0].Content), "tools")
	})

	t.Run("command", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeCommand, "d/commands/fix.md", f2Command))
		require.Equal(t, ".gemini/commands/fix.toml", res.Files[0].Path)
		s := string(res.Files[0].Content)
		require.Contains(t, s, `description = "Fix it"`)
		require.Contains(t, s, `prompt = "Fix {{args}} now, then $1.\n"`)
		require.Len(t, res.Warnings, 1)
	})

	t.Run("hook", func(t *testing.T) {
		res := tr.Transform(f2Hook("PreToolUse", "Bash|Write", "5"))
		require.Len(t, res.Hooks, 1)
		p := res.Hooks[0]
		require.Equal(t, ".gemini/settings.json", p.File)
		require.Equal(t, []string{"hooks"}, p.Keys)
		require.Equal(t, "BeforeTool", p.Event)
		require.JSONEq(t, `{"matcher":"run_shell_command|write_file","hooks":[{"type":"command","command":"go vet ./...","timeout":5000}]}`, string(p.Entry))
	})

	t.Run("hook events", func(t *testing.T) {
		for claude, want := range map[string]string{
			"PostToolUse": "AfterTool", "UserPromptSubmit": "BeforeAgent", "Stop": "AfterAgent",
			"SessionStart": "SessionStart", "SessionEnd": "SessionEnd", "PreCompact": "PreCompress", "Notification": "Notification",
		} {
			res := tr.Transform(f2Hook(claude, "", ""))
			require.Equal(t, want, res.Hooks[0].Event)
			require.NotContains(t, f2Entry(t, res.Hooks[0].Entry), "matcher")
		}
	})

	t.Run("hook unsupported event", func(t *testing.T) {
		res := tr.Transform(f2Hook("SubagentStop", "", ""))
		require.True(t, res.Skipped)
		require.NotEmpty(t, res.Warnings)
	})
}
