package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

type namedTransformer string

func (n namedTransformer) ToolName() string { return string(n) }
func (n namedTransformer) Detect() []string { return nil }
func (n namedTransformer) Transform(domain.TransformInput) domain.TransformResult {
	return domain.TransformResult{ToolName: string(n)}
}

func TestTransformerRegistry_EnabledTransformersSortedAndFiltered(t *testing.T) {
	reg := domain.TransformerRegistry{"pi": namedTransformer("pi"), "codex": namedTransformer("codex"), "gemini": namedTransformer("gemini")}

	got := reg.EnabledTransformers(map[string]bool{"pi": true, "codex": true, "gemini": false, "unknown": true})

	require.Len(t, got, 2)
	require.Equal(t, "codex", got[0].ToolName())
	require.Equal(t, "pi", got[1].ToolName())
	require.Equal(t, []string{"codex", "gemini", "pi"}, reg.Names())
}

func TestHookPatch_Anchor(t *testing.T) {
	p := domain.HookPatch{File: ".codex/hooks.json", Keys: []string{"hooks"}, Event: "PreToolUse"}
	require.Equal(t, ".codex/hooks.json#hooks/PreToolUse", p.Anchor())
}

func TestOutputFile_IsShared(t *testing.T) {
	require.True(t, domain.OutputFile{Path: ".agents/skills/x/SKILL.md"}.IsShared())
	require.False(t, domain.OutputFile{Path: ".pi/prompts/x.md"}.IsShared())
}
