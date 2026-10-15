package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestDeepCode_Basics(t *testing.T) {
	tr := transform.DeepCodeTransformer{}
	require.Equal(t, "deepcode", tr.ToolName())
	require.Equal(t, []string{".deepcode"}, tr.Detect())
}

func TestDeepCode_Skill(t *testing.T) {
	res := transform.DeepCodeTransformer{}.Transform(f1Input(domain.EntryTypeSkill, "dev/skills/go.md", f1Skill))
	require.Len(t, res.Files, 1)
	require.Equal(t, ".deepcode/skills/go/SKILL.md", res.Files[0].Path)
	fm, body := f1Split(t, res.Files[0].Content)
	require.Equal(t, "go", fm["name"])
	require.Equal(t, "Use gofmt.\n", body)
}

func TestDeepCode_Command(t *testing.T) {
	res := transform.DeepCodeTransformer{}.Transform(f1Input(domain.EntryTypeCommand, "dev/commands/fix.md", f1Command))
	require.Len(t, res.Files, 1)
	require.Equal(t, ".deepcode/skills/fix/SKILL.md", res.Files[0].Path)
	fm, body := f1Split(t, res.Files[0].Content)
	require.Equal(t, map[string]any{"name": "fix", "description": "Fix an issue", "argument-hint": "<issue>"}, fm)
	require.Equal(t, "Fix $ARGUMENTS then $1.\n", body)
	require.Len(t, res.Warnings, 1)
	require.Contains(t, res.Warnings[0], "unverified")

	res = transform.DeepCodeTransformer{}.Transform(f1Input(domain.EntryTypeCommand, "dev/commands/lint.md", "Run the linter.\n"))
	require.Empty(t, res.Warnings)
}

func TestDeepCode_Unsupported(t *testing.T) {
	for _, in := range []domain.TransformInput{
		f1Input(domain.EntryTypeAgent, "dev/agents/reviewer.md", f1Agent),
		f1Hook(`{"event":"Stop","hooks":[{"type":"command","command":"x"}]}`),
	} {
		res := transform.DeepCodeTransformer{}.Transform(in)
		require.True(t, res.Skipped)
		require.Empty(t, res.Files)
	}
}
