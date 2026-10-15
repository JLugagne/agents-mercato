package transform

import (
	"fmt"
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// WindsurfTransformer targets Windsurf (Devin Desktop). The base directory is
// .devin when the project already has one, .windsurf otherwise.
type WindsurfTransformer struct{}

func (WindsurfTransformer) ToolName() string { return "windsurf" }

func (WindsurfTransformer) Detect() []string {
	return []string{".windsurf", ".devin", ".windsurfrules"}
}

const windsurfWorkflowLimit = 12000

func windsurfBase(in domain.TransformInput) string {
	if in.Exists != nil && in.Exists(".devin") {
		return ".devin"
	}
	return ".windsurf"
}

func (WindsurfTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "windsurf"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(tool, sharedSkillFiles(in))
	case domain.EntryTypeCommand:
		content := workflowMarkdown(in)
		var warnings []string
		if len(content) > windsurfWorkflowLimit {
			warnings = append(warnings, fmt.Sprintf("windsurf: workflow exceeds %d characters and may be truncated", windsurfWorkflowLimit))
		}
		return files(tool, []domain.OutputFile{{Path: windsurfBase(in) + "/workflows/" + entryStem(in.Entry) + ".md", Content: content}}, warnings...)
	case domain.EntryTypeHook:
		return windsurfHook(in)
	}
	return unsupported(tool, in.Entry.Type)
}

func windsurfKind(name string) string {
	switch name {
	case "Bash":
		return "run_command"
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return "write_code"
	case "Read", "Grep", "Glob", "LS":
		return "read_code"
	}
	if strings.HasPrefix(name, "mcp__") {
		return "mcp_tool_use"
	}
	return ""
}

var windsurfAllKinds = []string{"run_command", "write_code", "read_code", "mcp_tool_use"}

func windsurfHook(in domain.TransformInput) domain.TransformResult {
	const tool = "windsurf"
	h, warnings, err := parseHook(in)
	if err != nil {
		return hookParseSkip(tool, err)
	}
	var events []string
	switch h.Event {
	case "PreToolUse", "PostToolUse":
		prefix := "pre_"
		if h.Event == "PostToolUse" {
			prefix = "post_"
		}
		kinds := map[string]bool{}
		names, ok := matcherTools(h.Matcher)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("windsurf: hook matcher %q is not a plain tool list, hook applies to all tool kinds", h.Matcher))
			names = nil
		}
		var unknown []string
		for _, n := range names {
			if k := windsurfKind(n); k != "" {
				kinds[k] = true
			} else {
				unknown = append(unknown, n)
			}
		}
		warnings = append(warnings, unknownToolsWarning(tool, unknown)...)
		if names == nil {
			for _, k := range windsurfAllKinds {
				kinds[k] = true
			}
		}
		for _, k := range windsurfAllKinds {
			if kinds[k] {
				events = append(events, prefix+k)
			}
		}
	case "UserPromptSubmit":
		events = []string{"pre_user_prompt"}
	case "Stop":
		events = []string{"post_cascade_response"}
	default:
		return skipped(tool, fmt.Sprintf("hook event %q has no windsurf equivalent", h.Event))
	}
	if len(events) == 0 {
		return skipped(tool, "hook matcher selects no windsurf-supported tool")
	}
	file := windsurfBase(in) + "/hooks.json"
	seen := map[string]bool{}
	var patches []domain.HookPatch
	for _, ev := range events {
		for _, hd := range h.Handlers {
			key := ev + "\x00" + hd.Command
			if seen[key] {
				continue
			}
			seen[key] = true
			patches = append(patches, domain.HookPatch{
				File:  file,
				Keys:  []string{"hooks"},
				Event: ev,
				Entry: mustJSON(map[string]any{"command": hd.Command, "show_output": true}),
			})
		}
	}
	return domain.TransformResult{ToolName: tool, Hooks: patches, Warnings: warnings}
}
