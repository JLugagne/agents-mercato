package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// ContinueTransformer is a legacy target kept for existing installs: Continue
// is no longer maintained. Only skills are converted, into .continue/rules.
type ContinueTransformer struct{}

func (ContinueTransformer) ToolName() string { return "continue" }

func (ContinueTransformer) Detect() []string { return []string{".continue"} }

func (ContinueTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	if in.Entry.Type != domain.EntryTypeSkill {
		return unsupported("continue", in.Entry.Type)
	}
	name := skillName(entryStem(in.Entry))
	d := parseDoc(in.Content)
	out := renderMarkdown([]field{{"name", name}, {"description", d.str("description")}, {"alwaysApply", false}}, d.body)
	return files("continue", []domain.OutputFile{{Path: ".continue/rules/" + name + ".md", Content: out}})
}
