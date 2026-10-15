package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// AugmentTransformer targets Augment (Auggie). Skills and commands are read
// natively from .claude/.
type AugmentTransformer struct{}

func (AugmentTransformer) ToolName() string { return "augment" }

func (AugmentTransformer) Detect() []string { return []string{".augment"} }

func (AugmentTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "augment"
	switch in.Entry.Type {
	case domain.EntryTypeSkill, domain.EntryTypeCommand:
		return domain.TransformResult{ToolName: tool}
	case domain.EntryTypeAgent:
		d := parseDoc(in.Content)
		name, desc := agentIdentity(in, d)
		out := renderMarkdown([]field{{"name", name}, {"description", desc}}, d.body)
		var warnings []string
		if hasToolsField(d) {
			warnings = append(warnings, "augment: tool restrictions not converted (tool names differ)")
		}
		return files(tool, []domain.OutputFile{{Path: ".augment/agents/" + entryStem(in.Entry) + ".md", Content: out}}, warnings...)
	case domain.EntryTypeHook:
		return skipped(tool, "augment hooks require script files with a .sh/.ps1 extension; not converted")
	}
	return unsupported(tool, in.Entry.Type)
}
