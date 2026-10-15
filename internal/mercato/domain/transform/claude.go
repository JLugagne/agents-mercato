package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// ClaudeTransformer is the identity target. Claude Code files are written by
// the app's core install path, so it produces nothing here; it is registered
// so that "claude" is a valid tool name.
type ClaudeTransformer struct{}

func (ClaudeTransformer) ToolName() string { return "claude" }

func (ClaudeTransformer) Detect() []string { return nil }

func (ClaudeTransformer) Transform(domain.TransformInput) domain.TransformResult {
	return domain.TransformResult{ToolName: "claude"}
}
