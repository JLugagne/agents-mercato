package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestVibe(t *testing.T) {
	tr := transform.VibeTransformer{}
	require.Equal(t, "vibe", tr.ToolName())
	require.Equal(t, []string{".vibe"}, tr.Detect())

	r := tr.Transform(f3Input(domain.EntryTypeSkill, "skills/go/SKILL.md", "---\ndescription: d\n---\nb"))
	require.Equal(t, ".agents/skills/go/SKILL.md", r.Files[0].Path)

	r = tr.Transform(f3Input(domain.EntryTypeCommand, "commands/run.md", "---\ndescription: Run\n---\nGo"))
	require.Equal(t, ".vibe/skills/run/SKILL.md", r.Files[0].Path)
	fm := f3Frontmatter(t, r.Files[0].Content)
	require.Equal(t, true, fm["user-invocable"])
	require.Equal(t, true, fm["disable-model-invocation"])

	r = tr.Transform(f3Input(domain.EntryTypeAgent, "agents/rev.md", "---\nname: Rev\ndescription: \"Say \\\"hi\\\"\"\ntools: Read, Edit, MultiEdit, Bash, Grep, Task\n---\nLine1\nLine2\n"))
	require.Equal(t, ".vibe/agents/rev.toml", r.Files[0].Path)
	want := "display_name = \"Rev\"\n" +
		"description = \"Say \\\"hi\\\"\"\n" +
		"agent_type = \"subagent\"\n" +
		"instructions = \"Line1\\nLine2\\n\"\n" +
		"enabled_tools = [\"read_file\", \"edit\", \"bash\", \"grep\"]\n"
	require.Equal(t, want, string(r.Files[0].Content))
	require.Len(t, r.Warnings, 1)

	r = tr.Transform(f3Input(domain.EntryTypeAgent, "agents/x.md", "---\ndescription: D\n---\nB"))
	require.NotContains(t, string(r.Files[0].Content), "enabled_tools")

	require.True(t, tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("Stop", "", 0))).Skipped)
}
