package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

var copilotEvents = map[string]string{
	"PreToolUse":       "preToolUse",
	"PostToolUse":      "postToolUse",
	"UserPromptSubmit": "userPromptSubmitted",
	"Stop":             "agentStop",
	"SubagentStop":     "subagentStop",
	"SessionStart":     "sessionStart",
	"SessionEnd":       "sessionEnd",
}

// CopilotTransformer targets GitHub Copilot. Each hook becomes its own
// .github/hooks/mct-<name>.json file; Copilot hooks cannot filter by tool.
type CopilotTransformer struct{}

func (CopilotTransformer) ToolName() string { return "copilot" }

func (CopilotTransformer) Detect() []string {
	return []string{
		".github/copilot-instructions.md",
		".github/agents",
		".github/skills",
		".github/prompts",
		".github/instructions",
		".github/hooks",
	}
}

func (CopilotTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	switch in.Entry.Type {
	case domain.EntryTypeSkill, domain.EntryTypeAgent:
		return domain.TransformResult{ToolName: "copilot"}
	case domain.EntryTypeCommand:
		return files("copilot", commandSkillFiles(".github/skills", in, field{"disable-model-invocation", true}))
	case domain.EntryTypeHook:
		return copilotHook(in)
	}
	return unsupported("copilot", in.Entry.Type)
}

func copilotHook(in domain.TransformInput) domain.TransformResult {
	h, warnings, err := parseHook(in)
	if err != nil {
		return skipped("copilot", "invalid hook: "+err.Error())
	}
	event, ok := copilotEvents[h.Event]
	if !ok {
		warnings = append(warnings, "copilot: hook event \""+h.Event+"\" has no equivalent")
		return domain.TransformResult{ToolName: "copilot", Skipped: true, SkipReason: "hook event \"" + h.Event + "\" is not supported", Warnings: warnings}
	}
	if m, _ := asIsMatcher(h.Matcher, domain.ToolMapping{}); m != "" {
		warnings = append(warnings, "copilot hooks have no matcher, the hook runs for every tool")
	}
	entries := make([]map[string]any, 0, len(h.Handlers))
	for _, hd := range h.Handlers {
		e := map[string]any{"type": "command", "bash": hd.Command}
		if hd.Timeout > 0 {
			e["timeoutSec"] = jsonHookTimeout(hd.Timeout, 1)
		}
		entries = append(entries, e)
	}
	doc := map[string]any{"version": 1, "hooks": map[string]any{event: entries}}
	return files("copilot", []domain.OutputFile{{Path: ".github/hooks/mct-" + h.Name + ".json", Content: prettyJSON(doc)}}, warnings...)
}
