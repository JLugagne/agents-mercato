package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// TraeTransformer targets Trae; only skills are supported.
type TraeTransformer struct{}

func (TraeTransformer) ToolName() string { return "trae" }

func (TraeTransformer) Detect() []string { return []string{".trae"} }

func (TraeTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	if in.Entry.Type == domain.EntryTypeSkill {
		return files("trae", skillFiles(".trae/skills", in))
	}
	return unsupported("trae", in.Entry.Type)
}
