package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestCline(t *testing.T) {
	tr := transform.ClineTransformer{}
	require.Equal(t, "cline", tr.ToolName())
	require.Equal(t, []string{".clinerules", ".cline"}, tr.Detect())

	r := tr.Transform(f3Input(domain.EntryTypeSkill, "skills/s/SKILL.md", "x"))
	require.False(t, r.Skipped)
	require.Empty(t, r.Files)
	require.Empty(t, r.Warnings)

	r = tr.Transform(f3Input(domain.EntryTypeCommand, "commands/go.md", "---\ndescription: Go\n---\nDo $ARGUMENTS"))
	require.Equal(t, ".clinerules/workflows/go.md", r.Files[0].Path)
	require.Equal(t, "# Go\n\nDo the user's request", string(r.Files[0].Content))

	require.True(t, tr.Transform(f3Input(domain.EntryTypeAgent, "agents/a.md", "x")).Skipped)
	require.True(t, tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("Stop", "", 0))).Skipped)
}
