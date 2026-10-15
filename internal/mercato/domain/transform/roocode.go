package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// RooCodeTransformer is a legacy target kept for existing installs: Roo Code
// shut down in 2026-05. Only skills are converted, into .roo/rules.
type RooCodeTransformer struct{}

func (RooCodeTransformer) ToolName() string { return "roocode" }

func (RooCodeTransformer) Detect() []string { return []string{".roo", ".roorules"} }

func (RooCodeTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	if in.Entry.Type != domain.EntryTypeSkill {
		return unsupported("roocode", in.Entry.Type)
	}
	name := skillName(entryStem(in.Entry))
	body := parseDoc(in.Content).body
	out := []domain.OutputFile{{Path: ".roo/rules/" + name + ".md", Content: []byte("# " + name + "\n\n" + string(body))}}
	return files("roocode", out)
}
