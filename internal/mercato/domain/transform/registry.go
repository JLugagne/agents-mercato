package transform

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

// Registry returns every supported target keyed by tool name. RooCode and
// Continue are kept only so existing installs keep being updated.
func Registry() domain.TransformerRegistry {
	all := []domain.Transformer{
		ClaudeTransformer{},
		AntigravityTransformer{},
		AugmentTransformer{},
		ClineTransformer{},
		CodeWhaleTransformer{},
		CodexTransformer{},
		ContinueTransformer{},
		CopilotTransformer{},
		CursorTransformer{},
		DeepCodeTransformer{},
		FactoryTransformer{},
		GeminiTransformer{},
		JunieTransformer{},
		KiloTransformer{},
		KiroTransformer{},
		OpenCodeTransformer{},
		PiTransformer{},
		QwenTransformer{},
		RooCodeTransformer{},
		TraeTransformer{},
		VibeTransformer{},
		WindsurfTransformer{},
		ZedTransformer{},
	}
	reg := make(domain.TransformerRegistry, len(all))
	for _, t := range all {
		reg[t.ToolName()] = t
	}
	return reg
}
