package transform

import (
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

var factoryToolNames = map[string]string{
	"Read":         "Read",
	"LS":           "LS",
	"Grep":         "Grep",
	"Glob":         "Glob",
	"Write":        "Create",
	"Edit":         "Edit",
	"MultiEdit":    "Edit",
	"NotebookEdit": "Edit",
	"Bash":         "Execute",
	"WebFetch":     "FetchUrl",
	"WebSearch":    "WebSearch",
	"Task":         "Task",
	"TodoWrite":    "",
}

// FactoryTransformer targets Factory Droid. Droid files use Factory tool
// names; an empty model means the droid inherits the session model.
type FactoryTransformer struct{}

func (FactoryTransformer) ToolName() string { return "factory" }

func (FactoryTransformer) Detect() []string { return []string{".factory"} }

func (FactoryTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files("factory", sharedSkillFiles(in))
	case domain.EntryTypeAgent:
		return factoryAgent(in)
	case domain.EntryTypeCommand:
		d := parseDoc(in.Content)
		out := renderMarkdown([]field{{"description", d.str("description")}, {"argument-hint", d.str("argument-hint")}}, d.body)
		warnings := positionalWarning("factory", d.body, "are not supported by Factory commands and were kept verbatim")
		return files("factory", []domain.OutputFile{{Path: ".factory/commands/" + entryStem(in.Entry) + ".md", Content: out}}, warnings...)
	case domain.EntryTypeHook:
		return jsonHookTransform(jsonHookSpec{
			tool:          "factory",
			file:          ".factory/hooks.json",
			keys:          []string{},
			events:        claudeDefinedEvents("PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop", "SubagentStop", "SessionStart", "SessionEnd", "PreCompact", "Notification"),
			matcher:       mappedMatcher("factory", factoryToolNames),
			timeoutFactor: 1,
		}, in)
	}
	return unsupported("factory", in.Entry.Type)
}

func factoryAgent(in domain.TransformInput) domain.TransformResult {
	d := parseDoc(in.Content)
	stem := entryStem(in.Entry)
	name := agentSlug(d, stem)
	desc := d.str("description")
	if desc == "" {
		desc = name
	}
	model, warnings := mapModel("factory", d.str("model"), in.Mappings, nil)
	tools, unknown := mapTools("factory", d.list("tools"), in.Mappings, factoryToolNames)
	warnings = append(warnings, unknownToolsWarning("factory", unknown)...)
	body := d.body
	if strings.TrimSpace(string(body)) == "" {
		body = []byte(desc + "\n")
	}
	out := renderMarkdown([]field{{"name", name}, {"description", desc}, {"model", model}, {"tools", tools}}, body)
	return files("factory", []domain.OutputFile{{Path: ".factory/droids/" + stem + ".md", Content: out}}, warnings...)
}
