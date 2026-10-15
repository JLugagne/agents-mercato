package transform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

type jsonHookSpec struct {
	tool          string
	file          string
	keys          []string
	header        map[string]any
	events        map[string]string
	matcher       func(matcher string, mappings domain.ToolMapping) (string, []string)
	timeoutFactor float64
	flat          bool
}

func jsonHookTimeout(seconds, factor float64) any {
	v := math.Round(seconds*factor*1000) / 1000
	if v == math.Trunc(v) {
		return int64(v)
	}
	return v
}

func jsonHookGroup(matcher string, handlers []map[string]any) json.RawMessage {
	entry := map[string]any{"hooks": handlers}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	return mustJSON(entry)
}

func jsonHookTransform(spec jsonHookSpec, in domain.TransformInput) domain.TransformResult {
	h, warnings, err := parseHook(in)
	if err != nil {
		return skipped(spec.tool, "invalid hook: "+err.Error())
	}
	event, ok := spec.events[h.Event]
	if !ok {
		warnings = append(warnings, fmt.Sprintf("%s: hook event %q has no equivalent", spec.tool, h.Event))
		return domain.TransformResult{ToolName: spec.tool, Skipped: true, SkipReason: fmt.Sprintf("hook event %q is not supported", h.Event), Warnings: warnings}
	}
	matcher, mw := spec.matcher(h.Matcher, in.Mappings)
	warnings = append(warnings, mw...)
	res := domain.TransformResult{ToolName: spec.tool, Warnings: warnings}
	patch := func(entry json.RawMessage) domain.HookPatch {
		return domain.HookPatch{File: spec.file, Keys: spec.keys, Event: event, Entry: entry, Header: spec.header}
	}
	if spec.flat {
		for _, hd := range h.Handlers {
			entry := map[string]any{"type": "command", "command": hd.Command}
			if hd.Timeout > 0 {
				entry["timeout"] = jsonHookTimeout(hd.Timeout, spec.timeoutFactor)
			}
			if matcher != "" {
				entry["matcher"] = matcher
			}
			res.Hooks = append(res.Hooks, patch(mustJSON(entry)))
		}
		return res
	}
	handlers := make([]map[string]any, 0, len(h.Handlers))
	for _, hd := range h.Handlers {
		m := map[string]any{"type": "command", "command": hd.Command}
		if hd.Timeout > 0 {
			m["timeout"] = jsonHookTimeout(hd.Timeout, spec.timeoutFactor)
		}
		handlers = append(handlers, m)
	}
	res.Hooks = append(res.Hooks, patch(jsonHookGroup(matcher, handlers)))
	return res
}

func asIsMatcher(matcher string, _ domain.ToolMapping) (string, []string) {
	m := strings.TrimSpace(matcher)
	if m == "*" || m == ".*" {
		return "", nil
	}
	return m, nil
}

func mappedMatcher(tool string, defaults map[string]string) func(string, domain.ToolMapping) (string, []string) {
	return func(matcher string, mappings domain.ToolMapping) (string, []string) {
		return mapMatcher(tool, matcher, mappings, defaults)
	}
}

var nonAgentNameChars = regexp.MustCompile(`[^a-z0-9_-]+`)

func agentSlug(d doc, stem string) string {
	for _, c := range []string{d.str("name"), stem} {
		s := strings.Trim(nonAgentNameChars.ReplaceAllString(strings.ToLower(c), "-"), "-")
		if s != "" {
			return s
		}
	}
	return "agent"
}

func prettyJSON(v any) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, mustJSON(v), "", "  "); err != nil {
		panic(err)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

func positionalWarning(tool string, body []byte, note string) []string {
	if hasPositionalArgs(body) {
		return []string{fmt.Sprintf("%s: positional arguments ($1..$9) %s", tool, note)}
	}
	return nil
}

func claudeDefinedEvents(names ...string) map[string]string {
	m := map[string]string{}
	for _, n := range names {
		m[n] = n
	}
	return m
}
