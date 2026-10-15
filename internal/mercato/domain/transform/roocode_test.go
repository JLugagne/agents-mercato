package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestRooCode(t *testing.T) {
	tr := transform.RooCodeTransformer{}
	require.Equal(t, "roocode", tr.ToolName())
	require.Equal(t, []string{".roo", ".roorules"}, tr.Detect())
	r := tr.Transform(f3Input(domain.EntryTypeSkill, "skills/Go_Review/SKILL.md", "---\ndescription: d\n---\nUse it.\n"))
	require.Equal(t, ".roo/rules/go-review.md", r.Files[0].Path)
	require.Equal(t, "# go-review\n\nUse it.\n", string(r.Files[0].Content))
	for _, ty := range []domain.EntryType{domain.EntryTypeAgent, domain.EntryTypeCommand, domain.EntryTypeHook} {
		require.True(t, tr.Transform(f3Input(ty, "x/y.md", "x")).Skipped)
	}
}
