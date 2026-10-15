package transform

import (
	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// KiloTransformer targets Kilo Code v7 (OpenCode-based). Skills go to
// .kilo/skills, agents and commands mirror OpenCode under .kilo/, hooks
// become default-exported TypeScript plugins under .kilo/plugin/.
type KiloTransformer struct{}

func (KiloTransformer) ToolName() string { return "kilo" }

func (KiloTransformer) Detect() []string {
	return []string{".kilo", ".kilocode", "kilo.json", "kilo.jsonc"}
}

func (KiloTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "kilo"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return files(tool, skillFiles(".kilo/skills", in))
	case domain.EntryTypeAgent:
		return opencodeAgent(tool, ".kilo/agents", in)
	case domain.EntryTypeCommand:
		var warnings []string
		if hasPositionalArgs(parseDoc(in.Content).body) {
			warnings = append(warnings, "kilo: positional command arguments ($1..$9) are unverified")
		}
		return opencodeCommand(tool, ".kilo/commands", in, warnings...)
	case domain.EntryTypeHook:
		return opencodeHook(tool, ".kilo/plugin", `import type { Plugin } from "@kilocode/plugin"`, in, func(export, hookName, plugin string) string {
			return "const " + export + ": Plugin = " + plugin + "\nexport default { id: " + shimJSString("mct-"+hookName) + ", server: " + export + " }\n"
		})
	}
	return unsupported(tool, in.Entry.Type)
}
