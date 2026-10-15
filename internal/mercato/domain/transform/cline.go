package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// ClineTransformer targets Cline. Skills are read natively from .claude/skills.
type ClineTransformer struct{}

func (ClineTransformer) ToolName() string { return "cline" }

func (ClineTransformer) Detect() []string { return []string{".clinerules", ".cline"} }

func (ClineTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "cline"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return domain.TransformResult{ToolName: tool}
	case domain.EntryTypeCommand:
		return files(tool, []domain.OutputFile{{Path: ".clinerules/workflows/" + entryStem(in.Entry) + ".md", Content: workflowMarkdown(in)}})
	case domain.EntryTypeHook:
		return skipped(tool, "cline hooks are executable scripts with a different stdin/stdout contract; not converted")
	}
	return unsupported(tool, in.Entry.Type)
}
