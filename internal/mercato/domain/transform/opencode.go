package transform

import (
	"fmt"
	"maps"
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// OpenCodeTransformer targets OpenCode. Skills are native (OpenCode reads
// .claude/skills); agents and commands go to .opencode/, hooks become
// TypeScript plugins under .opencode/plugins/ that run the Claude commands.
type OpenCodeTransformer struct{}

func (OpenCodeTransformer) ToolName() string { return "opencode" }

func (OpenCodeTransformer) Detect() []string {
	return []string{".opencode", "opencode.json", "opencode.jsonc"}
}

func (OpenCodeTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "opencode"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return domain.TransformResult{ToolName: tool}
	case domain.EntryTypeAgent:
		return opencodeAgent(tool, ".opencode/agents", in)
	case domain.EntryTypeCommand:
		return opencodeCommand(tool, ".opencode/commands", in)
	case domain.EntryTypeHook:
		return opencodeHook(tool, ".opencode/plugins", `import type { Plugin } from "@opencode-ai/plugin"`, in, func(export, _, plugin string) string {
			return "export const " + export + ": Plugin = " + plugin
		})
	}
	return unsupported(tool, in.Entry.Type)
}

var opencodeToolNames = map[string]string{
	"Read":         "read",
	"Write":        "edit",
	"Edit":         "edit",
	"MultiEdit":    "edit",
	"NotebookEdit": "edit",
	"Bash":         "bash",
	"Glob":         "glob",
	"Grep":         "grep",
	"LS":           "list",
	"WebFetch":     "webfetch",
	"WebSearch":    "websearch",
	"Task":         "task",
	"TodoWrite":    "todowrite",
}

var opencodePermissionKeys = []string{"read", "edit", "bash", "glob", "grep", "list", "webfetch", "websearch", "task", "todowrite"}

var opencodeModels = map[string]string{
	"sonnet": "anthropic/claude-sonnet-5-5",
	"opus":   "anthropic/claude-opus-5-5",
	"haiku":  "anthropic/claude-haiku-5-5",
}

var opencodeClaudeTools = map[string]string{
	"bash":        "Bash",
	"edit":        "Edit",
	"write":       "Write",
	"read":        "Read",
	"grep":        "Grep",
	"glob":        "Glob",
	"list":        "LS",
	"webfetch":    "WebFetch",
	"websearch":   "WebSearch",
	"task":        "Task",
	"apply_patch": "Edit",
	"todowrite":   "TodoWrite",
}

func opencodeModel(tool, model string, mappings domain.ToolMapping) (string, []string) {
	defaults := opencodeModels
	if strings.HasPrefix(model, "claude-") {
		defaults = maps.Clone(opencodeModels)
		defaults[model] = "anthropic/" + model
	}
	return mapModel(tool, model, mappings, defaults)
}

func opencodePermission(tool string, names []string, mappings domain.ToolMapping) ([]field, []string) {
	mapped, unknown := mapTools(tool, names, mappings, opencodeToolNames)
	allowed := map[string]bool{}
	for _, m := range mapped {
		allowed[m] = true
	}
	var perms []field
	known := map[string]bool{}
	for _, k := range opencodePermissionKeys {
		known[k] = true
		if allowed[k] {
			perms = append(perms, field{k, "allow"})
		} else {
			perms = append(perms, field{k, "deny"})
		}
	}
	for _, m := range mapped {
		if !known[m] {
			perms = append(perms, field{m, "allow"})
		}
	}
	return perms, unknownToolsWarning(tool, unknown)
}

func opencodeAgent(tool, dir string, in domain.TransformInput) domain.TransformResult {
	stem := entryStem(in.Entry)
	d := parseDoc(in.Content)
	desc := d.str("description")
	if desc == "" {
		desc = in.Entry.Description
	}
	if desc == "" {
		desc = stem
	}
	model, warnings := opencodeModel(tool, d.str("model"), in.Mappings)
	fields := []field{{"description", desc}, {"mode", "subagent"}, {"model", model}}
	if names := d.list("tools"); len(names) > 0 {
		perms, w := opencodePermission(tool, names, in.Mappings)
		fields = append(fields, field{"permission", perms})
		warnings = append(warnings, w...)
	}
	out := domain.OutputFile{Path: dir + "/" + stem + ".md", Content: renderMarkdown(fields, d.body)}
	return files(tool, []domain.OutputFile{out}, warnings...)
}

func opencodeCommand(tool, dir string, in domain.TransformInput, extraWarnings ...string) domain.TransformResult {
	d := parseDoc(in.Content)
	model, warnings := opencodeModel(tool, d.str("model"), in.Mappings)
	fields := []field{{"description", d.str("description")}, {"model", model}}
	out := domain.OutputFile{Path: dir + "/" + entryStem(in.Entry) + ".md", Content: renderMarkdown(fields, d.body)}
	return files(tool, []domain.OutputFile{out}, append(warnings, extraWarnings...)...)
}

func opencodeHook(tool, dir, importLine string, in domain.TransformInput, export func(exportName, hookName, plugin string) string) domain.TransformResult {
	h, warnings, err := parseHook(in)
	if err != nil {
		return skipped(tool, "invalid hook: "+err.Error())
	}
	obj, toolEvent, prompt, ok := opencodePluginHooks(h)
	if !ok {
		return shimNoEvent(tool, h.Event)
	}
	if h.Event == "UserPromptSubmit" {
		warnings = append(warnings, fmt.Sprintf("%s: UserPromptSubmit mapped to the chat.message plugin hook (unverified, cannot block the prompt)", tool))
	}
	warnings = append(warnings, shimMatcherIgnored(tool, h, toolEvent)...)
	name := shimHookName(h)
	plugin := "async ({ directory }) => ({\n" + obj + "})\n"
	spec := shimSpec{
		imports:    []string{importLine},
		hook:       h,
		toolNames:  opencodeClaudeTools,
		toolEvent:  toolEvent,
		promptText: prompt,
		body:       export("Mct"+shimPascal(name), name, plugin),
	}
	return shimHookResult(tool, dir+"/mct-"+name+".ts", spec, warnings)
}

func opencodePluginHooks(h claudeHook) (obj string, toolEvent, prompt, ok bool) {
	const ind = "    "
	session := shimArg{"session_id", `field(input, "sessionID")`}
	cwd := shimArg{"cwd", "directory"}
	event := shimArg{"hook_event_name", shimJSString(h.Event)}
	switch h.Event {
	case "PreToolUse":
		return `  "tool.execute.before": async (input, output) => {
` + shimToolGuard(ind, `field(input, "tool")`, h) + ind + "const blocked = " + shimCall(ind, "directory", []shimArg{
			session, cwd, event,
			{"tool_name", "toolName"},
			{"tool_input", `field(output, "args")`},
		}) + `
    if (blocked !== undefined) throw new Error(blocked)
  },
`, true, false, true
	case "PostToolUse":
		return `  "tool.execute.after": async (input, output) => {
` + shimToolGuard(ind, `field(input, "tool")`, h) + ind + shimCall(ind, "directory", []shimArg{
			session, cwd, event,
			{"tool_name", "toolName"},
			{"tool_input", `field(input, "args")`},
			{"tool_response", `field(output, "output")`},
		}) + `
  },
`, true, false, true
	case "SessionStart", "Stop":
		ocEvent, extra := "session.created", shimArg{"source", `"startup"`}
		if h.Event == "Stop" {
			ocEvent, extra = "session.idle", shimArg{"stop_hook_active", "false"}
		}
		return `  event: async ({ event }) => {
    if (field(event, "type") !== ` + shimJSString(ocEvent) + `) return
    const properties = field(event, "properties")
` + ind + shimCall(ind, "directory", []shimArg{
			{"session_id", `field(properties, "sessionID") ?? field(field(properties, "info"), "id")`},
			cwd, event, extra,
		}) + `
  },
`, false, false, true
	case "PreCompact":
		return `  "experimental.session.compacting": async (input) => {
` + ind + shimCall(ind, "directory", []shimArg{session, cwd, event, {"trigger", `"auto"`}}) + `
  },
`, false, false, true
	case "UserPromptSubmit":
		return `  "chat.message": async (input, output) => {
` + ind + shimCall(ind, "directory", []shimArg{session, cwd, event, {"prompt", `promptText(field(output, "parts"))`}}) + `
  },
`, false, true, true
	}
	return "", false, false, false
}
