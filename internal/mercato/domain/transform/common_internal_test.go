package transform

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func TestParseDoc_ToolsAsCommaStringOrList(t *testing.T) {
	require.Equal(t, []string{"Read", "Grep", "Bash"}, parseDoc([]byte("---\ntools: Read, Grep, Bash\n---\nbody")).list("tools"))
	require.Equal(t, []string{"Read", "Grep"}, parseDoc([]byte("---\ntools:\n  - Read\n  - Grep\n---\nbody")).list("tools"))
	require.Equal(t, "body", string(parseDoc([]byte("---\na: b\n---\nbody")).body))
}

func TestRenderMarkdown_QuotesValuesAndSkipsEmpty(t *testing.T) {
	out := renderMarkdown([]field{{"description", "Review: Go code"}, {"model", ""}, {"mode", "subagent"}}, []byte("Body\n"))
	require.Equal(t, "---\ndescription: 'Review: Go code'\nmode: subagent\n---\nBody\n", string(out))
	d := parseDoc(out)
	require.Equal(t, "Review: Go code", d.str("description"))
}

func TestSkillFiles_ForcesNameAndCopiesAssets(t *testing.T) {
	in := domain.TransformInput{
		Entry:   domain.Entry{Type: domain.EntryTypeSkill, RelPath: "dev/go/skills/Go_Review/SKILL.md", Filename: "SKILL.md"},
		Content: []byte("---\nname: other\ndescription: d\nlicense: MIT\n---\nUse it.\n"),
		Assets:  []domain.TransformAsset{{Path: "references/a.md", Content: []byte("A")}},
	}
	out := sharedSkillFiles(in)
	require.Len(t, out, 2)
	require.Equal(t, ".agents/skills/go-review/SKILL.md", out[0].Path)
	d := parseDoc(out[0].Content)
	require.Equal(t, "go-review", d.str("name"))
	require.Equal(t, "MIT", d.str("license"))
	require.Equal(t, "Use it.\n", string(d.body))
	require.Equal(t, ".agents/skills/go-review/references/a.md", out[1].Path)
}

func TestEntryStem(t *testing.T) {
	require.Equal(t, "flat", entryStem(domain.Entry{Type: domain.EntryTypeSkill, RelPath: "skills/flat.md"}))
	require.Equal(t, "rev", entryStem(domain.Entry{Type: domain.EntryTypeAgent, RelPath: "dev/go/agents/rev.md"}))
	require.Equal(t, "vet", entryStem(domain.Entry{Type: domain.EntryTypeHook, RelPath: "dev/go/hooks/vet.json"}))
}

func TestMapToolsAndModel_UserMappingWinsOverDefaults(t *testing.T) {
	m := domain.ToolMapping{
		Tools:  map[string]map[string]string{"Bash": {"x": "shell"}, "Write": {"x": ""}},
		Models: map[string]map[string]string{"opus": {"x": "big"}},
	}
	mapped, unknown := mapTools("x", []string{"Bash", "Read", "Write", "Nope", "Read"}, m, map[string]string{"Bash": "sh", "Read": "read"})
	require.Equal(t, []string{"shell", "read"}, mapped)
	require.Equal(t, []string{"Nope"}, unknown)

	v, w := mapModel("x", "opus", m, nil)
	require.Equal(t, "big", v)
	require.Empty(t, w)
	v, w = mapModel("x", "inherit", m, nil)
	require.Empty(t, v)
	require.Empty(t, w)
	v, w = mapModel("x", "sonnet", m, nil)
	require.Empty(t, v)
	require.Len(t, w, 1)
}

func TestMapMatcher(t *testing.T) {
	defaults := map[string]string{"Bash": "run_shell_command", "Edit": "replace", "Write": "write_file"}
	got, w := mapMatcher("gemini", "Edit|Write|mcp__gh__x", domain.ToolMapping{}, defaults)
	require.Equal(t, "replace|write_file|mcp__gh__x", got)
	require.Empty(t, w)
	got, _ = mapMatcher("gemini", "", domain.ToolMapping{}, defaults)
	require.Empty(t, got)
	got, w = mapMatcher("gemini", "Bash(git.*)", domain.ToolMapping{}, defaults)
	require.Equal(t, "Bash(git.*)", got)
	require.Len(t, w, 1)
}

func TestParseHook(t *testing.T) {
	in := domain.TransformInput{
		Entry:   domain.Entry{Type: domain.EntryTypeHook, RelPath: "hooks/go-vet.json"},
		Content: []byte(`{"event":"PostToolUse","matcher":"Edit|Write","hooks":[{"type":"command","command":"go vet ./...","timeout":30},{"type":"prompt","prompt":"x"}]}`),
	}
	h, warnings, err := parseHook(in)
	require.NoError(t, err)
	require.Equal(t, "go-vet", h.Name)
	require.Equal(t, "PostToolUse", h.Event)
	require.Equal(t, []hookHandler{{Command: "go vet ./...", Timeout: 30}}, h.Handlers)
	require.Len(t, warnings, 1)
}

func TestTomlString(t *testing.T) {
	require.Equal(t, `"a \"q\" \\ \n b"`, tomlString("a \"q\" \\ \n b"))
}
