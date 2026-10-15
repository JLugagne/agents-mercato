package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestAugment(t *testing.T) {
	tr := transform.AugmentTransformer{}
	require.Equal(t, "augment", tr.ToolName())
	require.Equal(t, []string{".augment"}, tr.Detect())
	for _, ty := range []domain.EntryType{domain.EntryTypeSkill, domain.EntryTypeCommand} {
		r := tr.Transform(f3Input(ty, "x/y.md", "x"))
		require.False(t, r.Skipped)
		require.Empty(t, r.Files)
		require.Empty(t, r.Warnings)
	}

	r := tr.Transform(f3Input(domain.EntryTypeAgent, "agents/rev.md", "---\nname: rev\ndescription: D\nmodel: opus\ntools: Read\n---\nBody\n"))
	require.Equal(t, ".augment/agents/rev.md", r.Files[0].Path)
	fm := f3Frontmatter(t, r.Files[0].Content)
	require.Equal(t, map[string]any{"name": "rev", "description": "D"}, fm)
	require.Len(t, r.Warnings, 1)

	r = tr.Transform(f3Input(domain.EntryTypeAgent, "agents/rev.md", "---\nname: rev\ndescription: D\n---\nBody\n"))
	require.Empty(t, r.Warnings)

	require.True(t, tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("Stop", "", 0))).Skipped)
}
