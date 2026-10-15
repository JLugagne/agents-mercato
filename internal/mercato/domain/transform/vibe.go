package transform

import (
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// VibeTransformer targets Mistral Vibe.
type VibeTransformer struct{}

func (VibeTransformer) ToolName() string { return "vibe" }

func (VibeTransformer) Detect() []string { return []string{".vibe"} }

var vibeToolNames = map[string]string{
	"Read": "read_file", "Write": "write_file", "Edit": "edit", "MultiEdit": "edit",
	"Bash": "bash", "Grep": "grep",
}

func (VibeTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "vibe"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(tool, sharedSkillFiles(in))
	case domain.EntryTypeCommand:
		return files(tool, commandSkillFiles(".vibe/skills", in, field{"user-invocable", true}, field{"disable-model-invocation", true}))
	case domain.EntryTypeAgent:
		d := parseDoc(in.Content)
		name, desc := agentIdentity(in, d)
		var b strings.Builder
		b.WriteString("display_name = " + tomlString(name) + "\n")
		b.WriteString("description = " + tomlString(desc) + "\n")
		b.WriteString("agent_type = \"subagent\"\n")
		b.WriteString("instructions = " + tomlString(string(d.body)) + "\n")
		var warnings []string
		if names := d.list("tools"); len(names) > 0 {
			tools, unknown := mapTools(tool, names, in.Mappings, vibeToolNames)
			b.WriteString("enabled_tools = " + tomlStringList(tools) + "\n")
			warnings = unknownToolsWarning(tool, unknown)
		}
		return files(tool, []domain.OutputFile{{Path: ".vibe/agents/" + entryStem(in.Entry) + ".toml", Content: []byte(b.String())}}, warnings...)
	case domain.EntryTypeHook:
		return skipped(tool, ".vibe/hooks.toml is shared TOML; not converted")
	}
	return unsupported(tool, in.Entry.Type)
}
