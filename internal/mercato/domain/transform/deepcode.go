package transform

import (
	"bytes"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// DeepCodeTransformer targets Deep Code (DeepSeek's agent integration).
// Skills go to .deepcode/skills and commands become skills there too, since
// Deep Code skills double as slash commands. Agents and hooks are unsupported.
type DeepCodeTransformer struct{}

func (DeepCodeTransformer) ToolName() string { return "deepcode" }

func (DeepCodeTransformer) Detect() []string { return []string{".deepcode"} }

func (DeepCodeTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "deepcode"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(tool, skillFiles(".deepcode/skills", in))
	case domain.EntryTypeCommand:
		var warnings []string
		if body := parseDoc(in.Content).body; bytes.Contains(body, []byte("$ARGUMENTS")) || hasPositionalArgs(body) {
			warnings = append(warnings, "deepcode: command argument substitution ($ARGUMENTS, $1..$9) is unverified")
		}
		return files(tool, commandSkillFiles(".deepcode/skills", in), warnings...)
	}
	return unsupported(tool, in.Entry.Type)
}
