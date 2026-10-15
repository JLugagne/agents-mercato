package transform

import (
	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// PiTransformer targets pi (earendil-works/pi). Skills go to the shared
// .agents/skills, commands become prompt templates under .pi/prompts and
// hooks become TypeScript extensions under .pi/extensions. Pi has no
// subagents, so agents are unsupported.
type PiTransformer struct{}

func (PiTransformer) ToolName() string { return "pi" }

func (PiTransformer) Detect() []string { return []string{".pi"} }

func (PiTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "pi"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(tool, sharedSkillFiles(in))
	case domain.EntryTypeCommand:
		d := parseDoc(in.Content)
		fields := []field{{"description", d.str("description")}, {"argument-hint", d.str("argument-hint")}}
		out := domain.OutputFile{Path: ".pi/prompts/" + entryStem(in.Entry) + ".md", Content: renderMarkdown(fields, d.body)}
		return files(tool, []domain.OutputFile{out})
	case domain.EntryTypeHook:
		return piHook(tool, in)
	}
	return unsupported(tool, in.Entry.Type)
}

var piClaudeTools = map[string]string{
	"bash":  "Bash",
	"read":  "Read",
	"edit":  "Edit",
	"write": "Write",
	"grep":  "Grep",
	"find":  "Glob",
	"ls":    "LS",
}

func piHook(tool string, in domain.TransformInput) domain.TransformResult {
	h, warnings, err := parseHook(in)
	if err != nil {
		return skipped(tool, "invalid hook: "+err.Error())
	}
	body, toolEvent, ok := piExtensionBody(h)
	if !ok {
		return shimNoEvent(tool, h.Event)
	}
	warnings = append(warnings, shimMatcherIgnored(tool, h, toolEvent)...)
	spec := shimSpec{
		imports:   []string{`import type { ExtensionAPI } from "@earendil-works/pi-coding-agent"`},
		hook:      h,
		toolNames: piClaudeTools,
		toolEvent: toolEvent,
		body:      "export default function (pi: ExtensionAPI) {\n" + body + "}\n",
	}
	return shimHookResult(tool, ".pi/extensions/mct-"+shimHookName(h)+".ts", spec, warnings)
}

func piExtensionBody(h claudeHook) (body string, toolEvent, ok bool) {
	const ind = "    "
	cwd := shimArg{"cwd", "process.cwd()"}
	event := shimArg{"hook_event_name", shimJSString(h.Event)}
	simple := func(piEvent string, args ...shimArg) string {
		return "  pi.on(" + shimJSString(piEvent) + ", async (event) => {\n" + ind +
			shimCall(ind, "process.cwd()", append([]shimArg{cwd, event}, args...)) + "\n  })\n"
	}
	switch h.Event {
	case "PreToolUse":
		return `  pi.on("tool_call", async (event) => {
` + shimToolGuard(ind, `field(event, "toolName")`, h) + ind + "const blocked = " + shimCall(ind, "process.cwd()", []shimArg{
			cwd, event,
			{"tool_name", "toolName"},
			{"tool_input", `field(event, "input")`},
		}) + `
    if (blocked !== undefined) return { block: true, reason: blocked }
  })
`, true, true
	case "PostToolUse":
		return `  pi.on("tool_result", async (event) => {
` + shimToolGuard(ind, `field(event, "toolName")`, h) + ind + shimCall(ind, "process.cwd()", []shimArg{
			cwd, event,
			{"tool_name", "toolName"},
			{"tool_input", `field(event, "input")`},
			{"tool_response", `field(event, "content")`},
		}) + `
  })
`, true, true
	case "SessionStart":
		return simple("session_start", shimArg{"source", `"startup"`}), false, true
	case "SessionEnd":
		return simple("session_shutdown", shimArg{"reason", `"other"`}), false, true
	case "Stop":
		return simple("agent_end", shimArg{"stop_hook_active", "false"}), false, true
	case "UserPromptSubmit":
		return simple("input", shimArg{"prompt", `field(event, "text")`}), false, true
	}
	return "", false, false
}
