package transform_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestAntigravity_SkillGoesToSharedDir(t *testing.T) {
	res := transform.AntigravityTransformer{}.Transform(f3Input(domain.EntryTypeSkill, "skills/rev/SKILL.md", "---\ndescription: d\n---\nbody\n"))
	require.False(t, res.Skipped)
	require.Len(t, res.Files, 1)
	require.Equal(t, ".agents/skills/rev/SKILL.md", res.Files[0].Path)
}

func TestAntigravity_CommandBecomesUserInvokedSkill(t *testing.T) {
	res := transform.AntigravityTransformer{}.Transform(f3Input(domain.EntryTypeCommand, "commands/ship.md", "---\ndescription: Ship it\n---\nShip $ARGUMENTS\n"))
	require.Len(t, res.Files, 1)
	require.Equal(t, ".agents/skills/ship/SKILL.md", res.Files[0].Path)
	fm := f3Frontmatter(t, res.Files[0].Content)
	require.Equal(t, "ship", fm["name"])
	require.Equal(t, true, fm["disable-model-invocation"])
}

func TestAntigravity_HookPatchesNamedBlock(t *testing.T) {
	res := transform.AntigravityTransformer{}.Transform(f3Input(domain.EntryTypeHook, "hooks/fmt.json", f3Hook("PostToolUse", "Edit|Write", 15)))
	require.False(t, res.Skipped)
	require.Len(t, res.Hooks, 1)
	p := res.Hooks[0]
	require.Equal(t, ".agents/hooks.json", p.File)
	require.Equal(t, []string{"mct-fmt"}, p.Keys)
	require.Equal(t, "PostToolUse", p.Event)
	var entry map[string]any
	require.NoError(t, json.Unmarshal(p.Entry, &entry))
	require.Equal(t, "Edit|Write", entry["matcher"])
	require.Equal(t, []any{map[string]any{"command": "echo hi", "timeout": float64(15)}}, entry["hooks"])
}

func TestAntigravity_UnsupportedEventAndAgent(t *testing.T) {
	require.True(t, transform.AntigravityTransformer{}.Transform(f3Input(domain.EntryTypeHook, "hooks/n.json", f3Hook("Notification", "", 0))).Skipped)
	require.True(t, transform.AntigravityTransformer{}.Transform(f3Input(domain.EntryTypeAgent, "agents/a.md", "---\ndescription: d\n---\nx\n")).Skipped)
}
