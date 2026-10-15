package transform_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestFactory(t *testing.T) {
	tr := transform.FactoryTransformer{}
	require.Equal(t, "factory", tr.ToolName())
	require.Equal(t, []string{".factory"}, tr.Detect())

	t.Run("skill", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeSkill, "d/skills/go-x/SKILL.md", f2Skill))
		require.Equal(t, ".agents/skills/go-x/SKILL.md", res.Files[0].Path)
	})

	t.Run("agent", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeAgent, "d/agents/reviewer.md", "---\nname: Code Reviewer\ndescription: Reviews\nmodel: inherit\ntools: Read, Write, Edit, MultiEdit, Bash, TodoWrite\n---\nYou review.\n"))
		require.Equal(t, ".factory/droids/reviewer.md", res.Files[0].Path)
		fm := f2Frontmatter(t, res.Files[0].Content)
		require.Equal(t, "code-reviewer", fm["name"])
		require.Equal(t, []any{"Read", "Create", "Edit", "Execute"}, fm["tools"])
		require.NotContains(t, fm, "model")
		require.Empty(t, res.Warnings)
	})

	t.Run("agent with unmapped model and unknown tool", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeAgent, "d/agents/reviewer.md", f2Agent))
		require.NotContains(t, f2Frontmatter(t, res.Files[0].Content), "model")
		require.Len(t, res.Warnings, 2)
	})

	t.Run("agent mapped model and empty body", func(t *testing.T) {
		in := f2Input(domain.EntryTypeAgent, "d/agents/a.md", "---\ndescription: Does things\nmodel: sonnet\n---\n")
		in.Mappings = domain.ToolMapping{Models: map[string]map[string]string{"sonnet": {"factory": "claude-sonnet-4-5"}}}
		res := tr.Transform(in)
		require.Equal(t, "claude-sonnet-4-5", f2Frontmatter(t, res.Files[0].Content)["model"])
		require.Contains(t, string(res.Files[0].Content), "---\nDoes things\n")
	})

	t.Run("command", func(t *testing.T) {
		res := tr.Transform(f2Input(domain.EntryTypeCommand, "d/commands/fix.md", f2Command))
		require.Equal(t, ".factory/commands/fix.md", res.Files[0].Path)
		fm := f2Frontmatter(t, res.Files[0].Content)
		require.Equal(t, "Fix it", fm["description"])
		require.Equal(t, "<issue>", fm["argument-hint"])
		require.Contains(t, string(res.Files[0].Content), "Fix $ARGUMENTS now")
		require.Len(t, res.Warnings, 1)
	})

	t.Run("hook", func(t *testing.T) {
		res := tr.Transform(f2Hook("PreToolUse", "Bash|Write", "10"))
		p := res.Hooks[0]
		require.Equal(t, ".factory/hooks.json", p.File)
		require.Empty(t, p.Keys)
		require.Equal(t, "PreToolUse", p.Event)
		require.Equal(t, "hooks.json#PreToolUse", p.Anchor()[len(".factory/"):])
		require.JSONEq(t, `{"matcher":"Execute|Create","hooks":[{"type":"command","command":"go vet ./...","timeout":10}]}`, string(p.Entry))
	})
}
