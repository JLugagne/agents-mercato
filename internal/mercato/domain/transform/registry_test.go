package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestRegistry_ListsEverySupportedTool(t *testing.T) {
	require.Equal(t, []string{
		"antigravity", "augment", "claude", "cline", "codewhale", "codex", "continue", "copilot", "cursor",
		"deepcode", "factory", "gemini", "junie", "kilo", "kiro", "opencode", "pi", "qwen", "roocode",
		"trae", "vibe", "windsurf", "zed",
	}, transform.Registry().Names())
}

func TestRegistry_EveryNonClaudeToolDetectsSomething(t *testing.T) {
	for name, tr := range transform.Registry() {
		if name == "claude" {
			continue
		}
		require.NotEmpty(t, tr.Detect(), name)
	}
}
