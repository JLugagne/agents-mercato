package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// ZedTransformer targets Zed; only skills are supported.
type ZedTransformer struct{}

func (ZedTransformer) ToolName() string { return "zed" }

func (ZedTransformer) Detect() []string { return []string{".zed"} }

func (ZedTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	if in.Entry.Type == domain.EntryTypeSkill {
		return files("zed", sharedSkillFiles(in))
	}
	return unsupported("zed", in.Entry.Type)
}
