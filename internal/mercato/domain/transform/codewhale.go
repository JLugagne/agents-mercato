package transform

import (
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// CodeWhaleTransformer targets CodeWhale (formerly DeepSeek-TUI). Skills are
// native (CodeWhale reads .claude/skills); agents become TOML role files
// under .codewhale/agents with the model dropped. Commands and hooks are
// unsupported.
type CodeWhaleTransformer struct{}

func (CodeWhaleTransformer) ToolName() string { return "codewhale" }

func (CodeWhaleTransformer) Detect() []string { return []string{".codewhale"} }

func (CodeWhaleTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	const tool = "codewhale"
	switch in.Entry.Type {
	case domain.EntryTypeSkill:
		return domain.TransformResult{ToolName: tool}
	case domain.EntryTypeAgent:
		return codewhaleAgent(tool, in)
	}
	return unsupported(tool, in.Entry.Type)
}

func codewhaleAgent(tool string, in domain.TransformInput) domain.TransformResult {
	stem := entryStem(in.Entry)
	d := parseDoc(in.Content)
	var b strings.Builder
	b.WriteString("id = " + tomlString(stem) + "\n")
	if desc := d.str("description"); desc != "" {
		b.WriteString("role_hint = " + tomlString(desc) + "\n")
	}
	b.WriteString("\n[instructions]\ntext = " + tomlString(string(d.body)) + "\n")
	var warnings []string
	if len(d.list("tools")) > 0 {
		warnings = append(warnings, "codewhale: agent tool restrictions are not converted")
	}
	out := domain.OutputFile{Path: ".codewhale/agents/" + stem + ".toml", Content: []byte(b.String())}
	return files(tool, []domain.OutputFile{out}, warnings...)
}
