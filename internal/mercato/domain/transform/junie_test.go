package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestJunie(t *testing.T) {
	tr := transform.JunieTransformer{}
	require.Equal(t, "junie", tr.ToolName())
	require.Equal(t, []string{".junie"}, tr.Detect())

	r := tr.Transform(f3Input(domain.EntryTypeSkill, "skills/go/SKILL.md", "---\ndescription: d\n---\nb"))
	require.Equal(t, ".agents/skills/go/SKILL.md", r.Files[0].Path)

	r = tr.Transform(f3Input(domain.EntryTypeAgent, "agents/rev.md", "---\nname: rev\nmodel: sonnet\ntools: Read, Bash, Task\n---\nBody\n"))
	require.Equal(t, ".junie/agents/rev.md", r.Files[0].Path)
	fm := f3Frontmatter(t, r.Files[0].Content)
	require.Equal(t, "rev", fm["name"])
	require.Equal(t, "rev", fm["description"])
	require.Equal(t, []any{"Read", "Bash"}, fm["tools"])
	require.Equal(t, "sonnet", fm["model"])
	require.Len(t, r.Warnings, 1)

	r = tr.Transform(f3Input(domain.EntryTypeAgent, "agents/rev.md", "---\ndescription: D\nmodel: claude-x\n---\nBody\n"))
	fm = f3Frontmatter(t, r.Files[0].Content)
	require.NotContains(t, fm, "model")
	require.NotContains(t, fm, "tools")
	require.Equal(t, "D", fm["description"])
	require.Len(t, r.Warnings, 1)

	r = tr.Transform(f3Input(domain.EntryTypeCommand, "commands/fix.md", "---\ndescription: Fix\n---\nFix $ARGUMENTS\n"))
	require.Equal(t, ".junie/commands/fix.md", r.Files[0].Path)
	fm = f3Frontmatter(t, r.Files[0].Content)
	require.Equal(t, true, fm["allowPromptArgument"])
	require.Equal(t, "Fix", fm["description"])
	require.Contains(t, string(r.Files[0].Content), "Fix $prompt\n")

	r = tr.Transform(f3Input(domain.EntryTypeCommand, "commands/p.md", "---\ndescription: P\n---\nNo args\n"))
	require.NotContains(t, f3Frontmatter(t, r.Files[0].Content), "allowPromptArgument")

	require.True(t, tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("Stop", "", 0))).Skipped)
}
