package transform

import (
	"fmt"
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

var geminiToolNames = map[string]string{
	"Read":      "read_file",
	"Write":     "write_file",
	"Edit":      "replace",
	"MultiEdit": "replace",
	"Bash":      "run_shell_command",
	"Grep":      "grep_search",
	"Glob":      "glob",
	"LS":        "list_directory",
	"WebFetch":  "web_fetch",
	"WebSearch": "google_web_search",
	"TodoWrite": "write_todos",
}

// GeminiTransformer targets Gemini CLI. Hook timeouts are converted from
// seconds to milliseconds.
type GeminiTransformer struct{}

func (GeminiTransformer) ToolName() string { return "gemini" }

func (GeminiTransformer) Detect() []string { return []string{".gemini", "GEMINI.md"} }

func (GeminiTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files("gemini", sharedSkillFiles(in))
	case domain.EntryTypeAgent:
		return geminiAgent(in)
	case domain.EntryTypeCommand:
		return geminiCommand(in)
	case domain.EntryTypeHook:
		return jsonHookTransform(jsonHookSpec{
			tool: "gemini",
			file: ".gemini/settings.json",
			keys: []string{"hooks"},
			events: map[string]string{
				"PreToolUse":       "BeforeTool",
				"PostToolUse":      "AfterTool",
				"UserPromptSubmit": "BeforeAgent",
				"Stop":             "AfterAgent",
				"SessionStart":     "SessionStart",
				"SessionEnd":       "SessionEnd",
				"PreCompact":       "PreCompress",
				"Notification":     "Notification",
			},
			matcher:       mappedMatcher("gemini", geminiToolNames),
			timeoutFactor: 1000,
		}, in)
	}
	return unsupported("gemini", in.Entry.Type)
}

func geminiAgent(in domain.TransformInput) domain.TransformResult {
	d := parseDoc(in.Content)
	name := agentSlug(d, entryStem(in.Entry))
	desc := d.str("description")
	if desc == "" {
		desc = name
	}
	var warnings []string
	tools, unknown := mapTools("gemini", d.list("tools"), in.Mappings, geminiToolNames)
	warnings = append(warnings, unknownToolsWarning("gemini", unknown)...)
	if m := d.str("model"); m != "" && m != "inherit" {
		warnings = append(warnings, fmt.Sprintf("gemini: model %q dropped (tool default model is used)", m))
	}
	out := renderMarkdown([]field{{"name", name}, {"description", desc}, {"tools", tools}}, d.body)
	return files("gemini", []domain.OutputFile{{Path: ".gemini/agents/" + entryStem(in.Entry) + ".md", Content: out}}, warnings...)
}

func geminiCommand(in domain.TransformInput) domain.TransformResult {
	d := parseDoc(in.Content)
	var b strings.Builder
	if desc := d.str("description"); desc != "" {
		fmt.Fprintf(&b, "description = %s\n", tomlString(desc))
	}
	fmt.Fprintf(&b, "prompt = %s\n", tomlString(string(replaceArguments(d.body, "{{args}}"))))
	warnings := positionalWarning("gemini", d.body, "are not supported by Gemini commands and were kept verbatim")
	return files("gemini", []domain.OutputFile{{Path: ".gemini/commands/" + entryStem(in.Entry) + ".toml", Content: []byte(b.String())}}, warnings...)
}
