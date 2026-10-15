package transform

import (
	"fmt"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

var qwenToolNames = map[string]string{
	"Read":      "read_file",
	"Write":     "write_file",
	"Edit":      "edit",
	"MultiEdit": "edit",
	"Bash":      "run_shell_command",
	"Grep":      "grep_search",
	"Glob":      "glob",
	"LS":        "list_directory",
	"WebFetch":  "web_fetch",
	"WebSearch": "web_search",
	"TodoWrite": "todo_write",
}

// QwenTransformer targets Qwen Code. Hooks keep Claude event names and
// second-based timeouts.
type QwenTransformer struct{}

func (QwenTransformer) ToolName() string { return "qwen" }

func (QwenTransformer) Detect() []string { return []string{".qwen", "QWEN.md"} }

func (QwenTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files("qwen", skillFiles(".qwen/skills", in))
	case domain.EntryTypeAgent:
		return qwenAgent(in)
	case domain.EntryTypeCommand:
		d := parseDoc(in.Content)
		out := renderMarkdown([]field{{"description", d.str("description")}}, replaceArguments(d.body, "{{args}}"))
		warnings := positionalWarning("qwen", d.body, "are not supported by Qwen commands and were kept verbatim")
		return files("qwen", []domain.OutputFile{{Path: ".qwen/commands/" + entryStem(in.Entry) + ".md", Content: out}}, warnings...)
	case domain.EntryTypeHook:
		return jsonHookTransform(jsonHookSpec{
			tool:          "qwen",
			file:          ".qwen/settings.json",
			keys:          []string{"hooks"},
			events:        claudeDefinedEvents("PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop", "SubagentStop", "SessionStart", "SessionEnd", "PreCompact", "Notification"),
			matcher:       mappedMatcher("qwen", qwenToolNames),
			timeoutFactor: 1,
		}, in)
	}
	return unsupported("qwen", in.Entry.Type)
}

func qwenAgent(in domain.TransformInput) domain.TransformResult {
	d := parseDoc(in.Content)
	stem := entryStem(in.Entry)
	name := agentSlug(d, stem)
	desc := d.str("description")
	if desc == "" {
		desc = name
	}
	tools, unknown := mapTools("qwen", d.list("tools"), in.Mappings, qwenToolNames)
	warnings := unknownToolsWarning("qwen", unknown)
	if m := d.str("model"); m != "" && m != "inherit" {
		warnings = append(warnings, fmt.Sprintf("qwen: model %q dropped (tool default model is used)", m))
	}
	out := renderMarkdown([]field{{"name", name}, {"description", desc}, {"tools", tools}}, d.body)
	return files("qwen", []domain.OutputFile{{Path: ".qwen/agents/" + stem + ".md", Content: out}}, warnings...)
}
