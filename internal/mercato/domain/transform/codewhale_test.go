package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestCodeWhale_Basics(t *testing.T) {
	tr := transform.CodeWhaleTransformer{}
	require.Equal(t, "codewhale", tr.ToolName())
	require.Equal(t, []string{".codewhale"}, tr.Detect())
}

func TestCodeWhale_SkillIsNative(t *testing.T) {
	res := transform.CodeWhaleTransformer{}.Transform(f1Input(domain.EntryTypeSkill, "dev/skills/go/SKILL.md", f1Skill))
	require.Equal(t, domain.TransformResult{ToolName: "codewhale"}, res)
}

func TestCodeWhale_Agent(t *testing.T) {
	in := f1Input(domain.EntryTypeAgent, "dev/agents/reviewer.md", "---\ndescription: Reviews \"Go\" code\nmodel: opus\ntools: Read\n---\nLine 1\n\tLine \\2\n")
	res := transform.CodeWhaleTransformer{}.Transform(in)
	require.Len(t, res.Files, 1)
	require.Equal(t, ".codewhale/agents/reviewer.toml", res.Files[0].Path)
	require.Equal(t, "id = \"reviewer\"\nrole_hint = \"Reviews \\\"Go\\\" code\"\n\n[instructions]\ntext = \"Line 1\\n\\tLine \\\\2\\n\"\n", string(res.Files[0].Content))
	require.NotContains(t, string(res.Files[0].Content), "opus")
	require.Equal(t, []string{"codewhale: agent tool restrictions are not converted"}, res.Warnings)

	res = transform.CodeWhaleTransformer{}.Transform(f1Input(domain.EntryTypeAgent, "dev/agents/plain.md", "Just a prompt.\n"))
	require.Equal(t, "id = \"plain\"\n\n[instructions]\ntext = \"Just a prompt.\\n\"\n", string(res.Files[0].Content))
	require.Empty(t, res.Warnings)
}

func TestCodeWhale_Unsupported(t *testing.T) {
	for _, in := range []domain.TransformInput{
		f1Input(domain.EntryTypeCommand, "dev/commands/fix.md", f1Command),
		f1Hook(`{"event":"Stop","hooks":[{"type":"command","command":"x"}]}`),
	} {
		res := transform.CodeWhaleTransformer{}.Transform(in)
		require.True(t, res.Skipped)
		require.Empty(t, res.Files)
	}
}
