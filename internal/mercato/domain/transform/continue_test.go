package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestContinue(t *testing.T) {
	tr := transform.ContinueTransformer{}
	require.Equal(t, "continue", tr.ToolName())
	require.Equal(t, []string{".continue"}, tr.Detect())
	r := tr.Transform(f3Input(domain.EntryTypeSkill, "skills/go/SKILL.md", "---\ndescription: \"Go: rules\"\n---\nUse it.\n"))
	require.Equal(t, ".continue/rules/go.md", r.Files[0].Path)
	fm := f3Frontmatter(t, r.Files[0].Content)
	require.Equal(t, "go", fm["name"])
	require.Equal(t, "Go: rules", fm["description"])
	require.Equal(t, false, fm["alwaysApply"])
	require.Contains(t, string(r.Files[0].Content), "---\nUse it.\n")
	for _, ty := range []domain.EntryType{domain.EntryTypeAgent, domain.EntryTypeCommand, domain.EntryTypeHook} {
		require.True(t, tr.Transform(f3Input(ty, "x/y.md", "x")).Skipped)
	}
}
