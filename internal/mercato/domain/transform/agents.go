package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// AgentsTransformer targets the vendor-neutral .agents/skills directory read
// by most coding tools. It is always detected, so enabling it creates
// .agents/skills even in a project that has no .agents directory yet.
type AgentsTransformer struct{}

const agentsTool = "agents"

func (AgentsTransformer) ToolName() string { return agentsTool }

func (AgentsTransformer) Detect() []string { return nil }

func (AgentsTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(agentsTool, sharedSkillFiles(in))
	case domain.EntryTypeCommand:
		return files(agentsTool, commandSkillFiles(".agents/skills", in, field{"disable-model-invocation", true}))
	}
	return unsupported(agentsTool, in.Entry.Type)
}
