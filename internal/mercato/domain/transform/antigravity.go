package transform

import (
	"fmt"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// AntigravityTransformer targets Google Antigravity. Commands become skills
// because Antigravity retires workflows on 2026-10-19.
type AntigravityTransformer struct{}

const antigravityTool = "antigravity"

var antigravityEvents = map[string]bool{"SessionStart": true, "PreToolUse": true, "PostToolUse": true, "Stop": true}

func (AntigravityTransformer) ToolName() string { return antigravityTool }

func (AntigravityTransformer) Detect() []string { return []string{".agents", ".agent"} }

func (AntigravityTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(antigravityTool, sharedSkillFiles(in))
	case domain.EntryTypeCommand:
		return files(antigravityTool, commandSkillFiles(".agents/skills", in, field{"disable-model-invocation", true}))
	case domain.EntryTypeHook:
		return antigravityHook(in)
	}
	return unsupported(antigravityTool, in.Entry.Type)
}

func antigravityHook(in domain.TransformInput) domain.TransformResult {
	h, warnings, err := parseHook(in)
	if err != nil {
		return skipped(antigravityTool, "invalid hook: "+err.Error())
	}
	if !antigravityEvents[h.Event] {
		return skipped(antigravityTool, fmt.Sprintf("hook event %s has no antigravity equivalent", h.Event))
	}
	handlers := make([]map[string]any, 0, len(h.Handlers))
	for _, hd := range h.Handlers {
		m := map[string]any{"command": hd.Command}
		if hd.Timeout > 0 {
			m["timeout"] = hd.Timeout
		}
		handlers = append(handlers, m)
	}
	entry := map[string]any{"hooks": handlers}
	if h.Matcher != "" {
		entry["matcher"] = h.Matcher
		warnings = append(warnings, fmt.Sprintf("%s: hook matcher %q kept as-is (antigravity tool names differ)", antigravityTool, h.Matcher))
	}
	return domain.TransformResult{
		ToolName: antigravityTool,
		Hooks: []domain.HookPatch{{
			File:  ".agents/hooks.json",
			Keys:  []string{"mct-" + h.Name},
			Event: h.Event,
			Entry: mustJSON(entry),
		}},
		Warnings: warnings,
	}
}
