package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// CursorTransformer targets Cursor. Skills and agents are read natively from
// .claude/; commands become skills hidden from model invocation.
type CursorTransformer struct{}

func (CursorTransformer) ToolName() string { return "cursor" }

func (CursorTransformer) Detect() []string { return []string{".cursor", ".cursorrules"} }

func (CursorTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	switch in.Entry.Type {
	case domain.EntryTypeSkill, domain.EntryTypeAgent:
		return domain.TransformResult{ToolName: "cursor"}
	case domain.EntryTypeCommand:
		return files("cursor", commandSkillFiles(".cursor/skills", in, field{"disable-model-invocation", true}))
	case domain.EntryTypeHook:
		return jsonHookTransform(jsonHookSpec{
			tool:   "cursor",
			file:   ".cursor/hooks.json",
			keys:   []string{"hooks"},
			header: map[string]any{"version": 1},
			events: map[string]string{
				"PreToolUse":       "preToolUse",
				"PostToolUse":      "postToolUse",
				"UserPromptSubmit": "beforeSubmitPrompt",
				"Stop":             "stop",
				"SubagentStop":     "subagentStop",
				"SessionStart":     "sessionStart",
				"SessionEnd":       "sessionEnd",
				"PreCompact":       "preCompact",
			},
			matcher:       asIsMatcher,
			timeoutFactor: 1,
			flat:          true,
		}, in)
	}
	return unsupported("cursor", in.Entry.Type)
}
