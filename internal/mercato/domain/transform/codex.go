package transform

import (
	"fmt"
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// CodexTransformer targets OpenAI Codex CLI. Hooks are merged into
// .codex/hooks.json with their Claude event names and matchers unchanged.
type CodexTransformer struct{}

func (CodexTransformer) ToolName() string { return "codex" }

func (CodexTransformer) Detect() []string { return []string{".codex"} }

func (CodexTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files("codex", sharedSkillFiles(in))
	case domain.EntryTypeAgent:
		return codexAgent(in)
	case domain.EntryTypeCommand:
		return skipped("codex", "codex custom prompts are user-level only and deprecated; use a skill")
	case domain.EntryTypeHook:
		return jsonHookTransform(jsonHookSpec{
			tool:          "codex",
			file:          ".codex/hooks.json",
			keys:          []string{"hooks"},
			events:        claudeDefinedEvents("PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop", "SubagentStop", "SessionStart", "SessionEnd", "PreCompact"),
			matcher:       asIsMatcher,
			timeoutFactor: 1,
		}, in)
	}
	return unsupported("codex", in.Entry.Type)
}

func codexAgent(in domain.TransformInput) domain.TransformResult {
	d := parseDoc(in.Content)
	stem := entryStem(in.Entry)
	name := agentSlug(d, stem)
	desc := d.str("description")
	if desc == "" {
		desc = name
	}
	var warnings []string
	if m := d.str("model"); m != "" && m != "inherit" {
		warnings = append(warnings, fmt.Sprintf("codex: model %q dropped (Codex uses OpenAI models)", m))
	}
	if len(d.list("tools")) > 0 {
		warnings = append(warnings, "codex: tool restrictions are not converted")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "name = %s\n", tomlString(name))
	fmt.Fprintf(&b, "description = %s\n", tomlString(desc))
	fmt.Fprintf(&b, "developer_instructions = %s\n", tomlString(string(d.body)))
	return files("codex", []domain.OutputFile{{Path: ".codex/agents/" + stem + ".toml", Content: []byte(b.String())}}, warnings...)
}
