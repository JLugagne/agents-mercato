package transform_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

func TestWindsurf_Basics(t *testing.T) {
	tr := transform.WindsurfTransformer{}
	require.Equal(t, "windsurf", tr.ToolName())
	require.Equal(t, []string{".windsurf", ".devin", ".windsurfrules"}, tr.Detect())
	require.True(t, tr.Transform(f3Input(domain.EntryTypeAgent, "agents/a.md", "x")).Skipped)
}

func TestWindsurf_SkillShared(t *testing.T) {
	r := transform.WindsurfTransformer{}.Transform(f3Input(domain.EntryTypeSkill, "skills/go/SKILL.md", "---\ndescription: d\n---\nbody"))
	require.Len(t, r.Files, 1)
	require.Equal(t, ".agents/skills/go/SKILL.md", r.Files[0].Path)
}

func TestWindsurf_CommandWorkflowAndBaseDir(t *testing.T) {
	in := f3Input(domain.EntryTypeCommand, "commands/fix.md", "---\ndescription: Fix it\n---\nDo: $ARGUMENTS\n")
	r := transform.WindsurfTransformer{}.Transform(in)
	require.Equal(t, ".windsurf/workflows/fix.md", r.Files[0].Path)
	require.Equal(t, "# Fix it\n\nDo: the user's request\n", string(r.Files[0].Content))

	in.Exists = func(p string) bool { return p == ".devin" }
	r = transform.WindsurfTransformer{}.Transform(in)
	require.Equal(t, ".devin/workflows/fix.md", r.Files[0].Path)

	r = transform.WindsurfTransformer{}.Transform(f3Input(domain.EntryTypeCommand, "commands/plain.md", "body"))
	require.True(t, strings.HasPrefix(string(r.Files[0].Content), "# plain\n\n"))

	r = transform.WindsurfTransformer{}.Transform(f3Input(domain.EntryTypeCommand, "commands/big.md", strings.Repeat("a", 12001)))
	require.NotEmpty(t, r.Warnings)
}

func TestWindsurf_HookEvents(t *testing.T) {
	tr := transform.WindsurfTransformer{}
	r := tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("PreToolUse", "Bash", 0)))
	require.Len(t, r.Hooks, 1)
	require.Equal(t, ".windsurf/hooks.json", r.Hooks[0].File)
	require.Equal(t, []string{"hooks"}, r.Hooks[0].Keys)
	require.Equal(t, "pre_run_command", r.Hooks[0].Event)
	require.JSONEq(t, `{"command":"echo hi","show_output":true}`, string(r.Hooks[0].Entry))

	r = tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("PostToolUse", "Edit|Write|Read|mcp__x", 5)))
	var evs []string
	for _, h := range r.Hooks {
		evs = append(evs, h.Event)
	}
	require.Equal(t, []string{"post_write_code", "post_read_code", "post_mcp_tool_use"}, evs)

	r = tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("PreToolUse", "", 0)))
	require.Len(t, r.Hooks, 4)

	r = tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("UserPromptSubmit", "", 0)))
	require.Equal(t, "pre_user_prompt", r.Hooks[0].Event)
	r = tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("Stop", "", 0)))
	require.Equal(t, "post_cascade_response", r.Hooks[0].Event)

	r = tr.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("SessionStart", "", 0)))
	require.True(t, r.Skipped)

	in := f3Input(domain.EntryTypeHook, "hooks/h.json", f3Hook("Stop", "", 0))
	in.Exists = func(p string) bool { return p == ".devin" }
	require.Equal(t, ".devin/hooks.json", tr.Transform(in).Hooks[0].File)
}

func TestWindsurf_HookInvalid(t *testing.T) {
	r := transform.WindsurfTransformer{}.Transform(f3Input(domain.EntryTypeHook, "hooks/h.json", "nope"))
	require.True(t, r.Skipped)
}
