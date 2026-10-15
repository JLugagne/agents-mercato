package transform

import (
	"bytes"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// JunieTransformer targets JetBrains Junie.
type JunieTransformer struct{}

func (JunieTransformer) ToolName() string { return "junie" }

func (JunieTransformer) Detect() []string { return []string{".junie"} }

var junieToolNames = map[string]string{
	"Read": "Read", "Bash": "Bash", "Glob": "Glob", "Grep": "Grep",
	"Write": "Write", "Edit": "Edit", "WebSearch": "WebSearch",
}

var junieModels = map[string]string{"sonnet": "sonnet", "opus": "opus", "haiku": "haiku"}

func (JunieTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "junie"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(tool, sharedSkillFiles(in))
	case domain.EntryTypeAgent:
		d := parseDoc(in.Content)
		name, desc := agentIdentity(in, d)
		if desc == "" {
			desc = entryStem(in.Entry)
		}
		tools, unknown := mapTools(tool, d.list("tools"), in.Mappings, junieToolNames)
		model, mw := mapModel(tool, d.str("model"), in.Mappings, junieModels)
		out := renderMarkdown([]field{{"name", name}, {"description", desc}, {"tools", tools}, {"model", model}}, d.body)
		return files(tool, []domain.OutputFile{{Path: ".junie/agents/" + entryStem(in.Entry) + ".md", Content: out}}, joinWarnings(unknownToolsWarning(tool, unknown), mw)...)
	case domain.EntryTypeCommand:
		d := parseDoc(in.Content)
		fields := []field{{"description", d.str("description")}}
		if bytes.Contains(d.body, []byte("$ARGUMENTS")) {
			fields = append(fields, field{"allowPromptArgument", true})
		}
		out := renderMarkdown(fields, replaceArguments(d.body, "$prompt"))
		return files(tool, []domain.OutputFile{{Path: ".junie/commands/" + entryStem(in.Entry) + ".md", Content: out}})
	case domain.EntryTypeHook:
		return skipped(tool, "junie ignores project-level hooks")
	}
	return unsupported(tool, in.Entry.Type)
}
