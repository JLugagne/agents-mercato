package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestAgents(t *testing.T) {
	tr := transform.AgentsTransformer{}
	require.Equal(t, "agents", tr.ToolName())
	require.Empty(t, tr.Detect())
	r := tr.Transform(f3Input(domain.EntryTypeSkill, "skills/go/SKILL.md", "---\ndescription: d\n---\nb"))
	require.Equal(t, ".agents/skills/go/SKILL.md", r.Files[0].Path)
	c := tr.Transform(f3Input(domain.EntryTypeCommand, "commands/run.md", "---\ndescription: d\n---\nb"))
	require.Equal(t, ".agents/skills/run/SKILL.md", c.Files[0].Path)
	for _, ty := range []domain.EntryType{domain.EntryTypeAgent, domain.EntryTypeHook} {
		require.True(t, tr.Transform(f3Input(ty, "x/y.md", "x")).Skipped)
	}
}
