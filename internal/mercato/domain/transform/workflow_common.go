package transform

import (
	"fmt"
	"math"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func workflowMarkdown(in domain.TransformInput) []byte {
	d := parseDoc(in.Content)
	title := d.str("description")
	if title == "" {
		title = entryStem(in.Entry)
	}
	body := replaceArguments(d.body, "the user's request")
	return []byte("# " + title + "\n\n" + string(body))
}

func agentIdentity(in domain.TransformInput, d doc) (name, description string) {
	name = d.str("name")
	if name == "" {
		name = entryStem(in.Entry)
	}
	return name, d.str("description")
}

func hookParseSkip(tool string, err error) domain.TransformResult {
	return skipped(tool, fmt.Sprintf("invalid hook snippet: %v", err))
}

func timeoutValue(t float64) any {
	if t == math.Trunc(t) {
		return int64(t)
	}
	return t
}

func joinWarnings(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func hasToolsField(d doc) bool {
	return len(d.list("tools")) > 0
}
