package cfgadapter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
)

func legacyInitMappings() domain.ToolMapping {
	empty := map[string]string{"cursor": "", "windsurf": "", "codex": "", "gemini": ""}
	model := func(opencode string) map[string]string {
		m := map[string]string{"opencode": opencode}
		for k, v := range empty {
			m[k] = v
		}
		return m
	}
	return domain.ToolMapping{
		Models: map[string]map[string]string{
			"opus":   model("anthropic/claude-opus-4-20250514"),
			"sonnet": model("anthropic/claude-sonnet-4-20250514"),
			"haiku":  model("anthropic/claude-haiku-4-5-20251001"),
		},
		Tools: map[string]map[string]string{
			"Read": {"opencode": "read"}, "Edit": {"opencode": "edit"}, "Bash": {"opencode": "bash"},
			"Write": {"opencode": "write"}, "Glob": {"opencode": "glob"}, "Grep": {"opencode": "grep"},
		},
	}
}

func TestToolMappings_LegacyInitDefaultsDoNotOverrideTransformerDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-mappings.yml")
	legacy := legacyInitMappings()
	legacy.Models["opus"]["gemini"] = "gemini-3-pro"
	data, err := yaml.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))

	m, err := cfgadapter.NewToolMappingStore().LoadToolMappings(path)
	require.NoError(t, err)

	require.Equal(t, map[string]map[string]string{"opus": {"gemini": "gemini-3-pro"}}, m.Models)
	require.Empty(t, m.Tools)
}

func TestToolMappings_DefaultsAreEmptyOverrides(t *testing.T) {
	d := cfgadapter.NewToolMappingStore().DefaultToolMappings()
	require.NotNil(t, d.Models)
	require.NotNil(t, d.Tools)
	require.Empty(t, d.Models)
	require.Empty(t, d.Tools)
}
