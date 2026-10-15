package transform

import (
	"fmt"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// KiroTransformer targets Kiro. Commands become skills because Kiro has no
// custom slash command files.
type KiroTransformer struct{}

func (KiroTransformer) ToolName() string { return "kiro" }

func (KiroTransformer) Detect() []string { return []string{".kiro"} }

var kiroToolNames = map[string]string{
	"Read": "read", "Grep": "read", "Glob": "read", "LS": "read",
	"Write": "write", "Edit": "write", "MultiEdit": "write", "NotebookEdit": "write",
	"Bash":     "shell",
	"WebFetch": "web", "WebSearch": "web",
	"Task": "subagent",
}

var kiroTriggers = map[string]bool{
	"PreToolUse": true, "PostToolUse": true, "SessionStart": true, "SessionEnd": true, "Stop": true,
}

func (KiroTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "kiro"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(tool, skillFiles(".kiro/skills", in))
	case domain.EntryTypeCommand:
		return files(tool, commandSkillFiles(".kiro/skills", in))
	case domain.EntryTypeAgent:
		d := parseDoc(in.Content)
		name, desc := agentIdentity(in, d)
		tools, unknown := mapTools(tool, d.list("tools"), in.Mappings, kiroToolNames)
		out := renderMarkdown([]field{{"name", name}, {"description", desc}, {"tools", tools}}, d.body)
		return files(tool, []domain.OutputFile{{Path: ".kiro/agents/" + entryStem(in.Entry) + ".md", Content: out}}, unknownToolsWarning(tool, unknown)...)
	case domain.EntryTypeHook:
		return kiroHook(in)
	}
	return unsupported(tool, in.Entry.Type)
}

func kiroHook(in domain.TransformInput) domain.TransformResult {
	const tool = "kiro"
	h, warnings, err := parseHook(in)
	if err != nil {
		return hookParseSkip(tool, err)
	}
	if !kiroTriggers[h.Event] {
		return skipped(tool, fmt.Sprintf("hook event %q has no kiro equivalent", h.Event))
	}
	matcher, mw := mapMatcher(tool, h.Matcher, in.Mappings, kiroToolNames)
	warnings = append(warnings, mw...)
	name := "mct-" + h.Name
	var items []map[string]any
	for _, hd := range h.Handlers {
		item := map[string]any{
			"name":    name,
			"trigger": h.Event,
			"action":  map[string]any{"type": "command", "command": hd.Command},
			"enabled": true,
		}
		if matcher != "" {
			item["matcher"] = matcher
		}
		if hd.Timeout > 0 {
			item["timeout"] = timeoutValue(hd.Timeout)
		}
		items = append(items, item)
	}
	content := append(mustJSON(map[string]any{"version": "v1", "hooks": items}), '\n')
	return files(tool, []domain.OutputFile{{Path: ".kiro/hooks/" + name + ".json", Content: content}}, warnings...)
}
