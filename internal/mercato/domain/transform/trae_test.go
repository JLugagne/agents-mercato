package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestTrae(t *testing.T) {
	tr := transform.TraeTransformer{}
	require.Equal(t, "trae", tr.ToolName())
	require.Equal(t, []string{".trae"}, tr.Detect())
	in := f3Input(domain.EntryTypeSkill, "skills/go/SKILL.md", "---\ndescription: d\n---\nb")
	in.Assets = []domain.TransformAsset{{Path: "ref/a.md", Content: []byte("A")}}
	r := tr.Transform(in)
	require.Len(t, r.Files, 2)
	require.Equal(t, ".trae/skills/go/SKILL.md", r.Files[0].Path)
	require.Equal(t, ".trae/skills/go/ref/a.md", r.Files[1].Path)
	for _, ty := range []domain.EntryType{domain.EntryTypeAgent, domain.EntryTypeCommand, domain.EntryTypeHook} {
		require.True(t, tr.Transform(f3Input(ty, "x/y.md", "x")).Skipped)
	}
}
