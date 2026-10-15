package transform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

type field struct {
	Key   string
	Value any
}

type doc struct {
	fm   *yaml.Node
	body []byte
}

func parseDoc(content []byte) doc {
	s := string(content)
	if !strings.HasPrefix(s, "---") {
		return doc{body: content}
	}
	end := strings.Index(s[3:], "\n---")
	if end == -1 {
		return doc{body: content}
	}
	rest := s[3+end+4:]
	rest = strings.TrimPrefix(rest, "\r")
	rest = strings.TrimPrefix(rest, "\n")
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(s[3:3+end]), &root); err != nil || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return doc{body: []byte(rest)}
	}
	return doc{fm: root.Content[0], body: []byte(rest)}
}

func (d doc) node(key string) *yaml.Node {
	if d.fm == nil {
		return nil
	}
	for i := 0; i+1 < len(d.fm.Content); i += 2 {
		if d.fm.Content[i].Value == key {
			return d.fm.Content[i+1]
		}
	}
	return nil
}

func (d doc) str(key string) string {
	n := d.node(key)
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(n.Value)
}

func (d doc) list(key string) []string {
	n := d.node(key)
	if n == nil {
		return nil
	}
	var raw []string
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			raw = append(raw, c.Value)
		}
	case yaml.ScalarNode:
		raw = strings.Split(n.Value, ",")
	}
	var out []string
	for _, v := range raw {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (d doc) withField(key string, value any) doc {
	fm := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if d.fm != nil {
		fm.Content = append(fm.Content, d.fm.Content...)
	}
	v := valueNode(value)
	for i := 0; i+1 < len(fm.Content); i += 2 {
		if fm.Content[i].Value == key {
			fm.Content[i+1] = v
			return doc{fm: fm, body: d.body}
		}
	}
	fm.Content = append([]*yaml.Node{scalar(key), v}, fm.Content...)
	return doc{fm: fm, body: d.body}
}

func (d doc) render() []byte {
	if d.fm == nil {
		return d.body
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(d.fm)
	_ = enc.Close()
	buf.WriteString("---\n")
	buf.Write(d.body)
	return buf.Bytes()
}

func scalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func valueNode(v any) *yaml.Node {
	switch x := v.(type) {
	case string:
		return scalar(x)
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(x)}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(x)}
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(x, 'f', -1, 64)}
	case []string:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, s := range x {
			n.Content = append(n.Content, scalar(s))
		}
		return n
	case []field:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, f := range x {
			n.Content = append(n.Content, scalar(f.Key), valueNode(f.Value))
		}
		return n
	default:
		return scalar(fmt.Sprint(x))
	}
}

func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []string:
		return len(x) == 0
	case []field:
		return len(x) == 0
	}
	return false
}

func renderMarkdown(fields []field, body []byte) []byte {
	fm := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, f := range fields {
		if isEmptyValue(f.Value) {
			continue
		}
		fm.Content = append(fm.Content, scalar(f.Key), valueNode(f.Value))
	}
	if len(fm.Content) == 0 {
		return body
	}
	return doc{fm: fm, body: body}.render()
}

var nonSkillNameChars = regexp.MustCompile(`[^a-z0-9]+`)

func skillName(s string) string {
	s = strings.Trim(nonSkillNameChars.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "-")
	}
	return s
}

func entryStem(e domain.Entry) string {
	rel := strings.TrimSuffix(e.RelPath, "/")
	base := path.Base(rel)
	if e.Type == domain.EntryTypeSkill && base == "SKILL.md" {
		return path.Base(path.Dir(rel))
	}
	if base == "." || base == "/" || base == "" {
		base = e.Filename
	}
	return strings.TrimSuffix(strings.TrimSuffix(base, ".md"), ".json")
}

func skillFiles(base string, in domain.TransformInput, extra ...field) []domain.OutputFile {
	name := skillName(entryStem(in.Entry))
	d := parseDoc(in.Content)
	if desc := d.str("description"); desc == "" {
		d = d.withField("description", name)
	}
	for i := len(extra) - 1; i >= 0; i-- {
		if !isEmptyValue(extra[i].Value) {
			d = d.withField(extra[i].Key, extra[i].Value)
		}
	}
	d = d.withField("name", name)
	dir := base + "/" + name
	files := []domain.OutputFile{{Path: dir + "/SKILL.md", Content: d.render()}}
	for _, a := range in.Assets {
		files = append(files, domain.OutputFile{Path: dir + "/" + strings.TrimPrefix(a.Path, "/"), Content: a.Content})
	}
	return files
}

func commandSkillFiles(base string, in domain.TransformInput, extra ...field) []domain.OutputFile {
	name := skillName(entryStem(in.Entry))
	d := parseDoc(in.Content)
	desc := d.str("description")
	if desc == "" {
		desc = "Run the " + name + " command"
	}
	fields := []field{{"name", name}, {"description", desc}}
	if hint := d.str("argument-hint"); hint != "" {
		fields = append(fields, field{"argument-hint", hint})
	}
	fields = append(fields, extra...)
	return []domain.OutputFile{{Path: base + "/" + name + "/SKILL.md", Content: renderMarkdown(fields, d.body)}}
}

func sharedSkillFiles(in domain.TransformInput, extra ...field) []domain.OutputFile {
	return skillFiles(".agents/skills", in, extra...)
}

func mapModel(tool, model string, mappings domain.ToolMapping, defaults map[string]string) (string, []string) {
	if model == "" || model == "inherit" {
		return "", nil
	}
	if byTool, ok := mappings.Models[model]; ok {
		if v, ok := byTool[tool]; ok {
			return v, nil
		}
	}
	if v, ok := defaults[model]; ok {
		return v, nil
	}
	return "", []string{fmt.Sprintf("%s: unmapped model %q dropped (tool default model is used)", tool, model)}
}

func mapTools(tool string, names []string, mappings domain.ToolMapping, defaults map[string]string) (mapped, unknown []string) {
	seen := map[string]bool{}
	for _, n := range names {
		v, ok := "", false
		if byTool, found := mappings.Tools[n]; found {
			v, ok = byTool[tool]
		}
		if !ok {
			v, ok = defaults[n]
		}
		if !ok {
			unknown = append(unknown, n)
			continue
		}
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		mapped = append(mapped, v)
	}
	return mapped, unknown
}

func unknownToolsWarning(tool string, unknown []string) []string {
	if len(unknown) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%s: no equivalent for tools %s, dropped", tool, strings.Join(unknown, ", "))}
}

var positionalArg = regexp.MustCompile(`\$[1-9]`)

func replaceArguments(body []byte, with string) []byte {
	return bytes.ReplaceAll(body, []byte("$ARGUMENTS"), []byte(with))
}

func hasPositionalArgs(body []byte) bool {
	return positionalArg.Match(body)
}

func skipped(tool, reason string) domain.TransformResult {
	return domain.TransformResult{ToolName: tool, Skipped: true, SkipReason: reason}
}

func unsupported(tool string, t domain.EntryType) domain.TransformResult {
	return skipped(tool, fmt.Sprintf("does not support %ss", t))
}

func files(tool string, out []domain.OutputFile, warnings ...string) domain.TransformResult {
	return domain.TransformResult{ToolName: tool, Files: out, Warnings: warnings}
}

type hookHandler struct {
	Command string
	Timeout float64
}

type claudeHook struct {
	Name     string
	Event    string
	Matcher  string
	Handlers []hookHandler
}

func parseHook(in domain.TransformInput) (claudeHook, []string, error) {
	snippet, err := domain.ParseHookSnippet(in.Content)
	if err != nil {
		return claudeHook{}, nil, err
	}
	h := claudeHook{Name: skillName(entryStem(in.Entry)), Event: snippet.Event, Matcher: snippet.Matcher}
	var warnings []string
	for _, raw := range snippet.Hooks {
		var x struct {
			Type    string      `json:"type"`
			Command string      `json:"command"`
			Timeout json.Number `json:"timeout"`
		}
		if err := json.Unmarshal(raw, &x); err != nil {
			return claudeHook{}, nil, err
		}
		if x.Type != "command" || x.Command == "" {
			warnings = append(warnings, fmt.Sprintf("hook handler type %q not convertible, dropped", x.Type))
			continue
		}
		t, _ := x.Timeout.Float64()
		h.Handlers = append(h.Handlers, hookHandler{Command: x.Command, Timeout: t})
	}
	if len(h.Handlers) == 0 {
		return claudeHook{}, warnings, errors.New("no command handler")
	}
	return h, warnings, nil
}

func matcherTools(matcher string) (tools []string, ok bool) {
	m := strings.TrimSpace(matcher)
	if m == "" || m == "*" || m == ".*" {
		return nil, true
	}
	for _, p := range strings.Split(m, "|") {
		p = strings.TrimSpace(p)
		if p == "" || strings.ContainsAny(p, `.*+?()[]{}^$\`) && !strings.HasPrefix(p, "mcp__") {
			return nil, false
		}
		tools = append(tools, p)
	}
	return tools, true
}

func mapMatcher(tool, matcher string, mappings domain.ToolMapping, defaults map[string]string) (string, []string) {
	names, ok := matcherTools(matcher)
	if !ok {
		return matcher, []string{fmt.Sprintf("%s: hook matcher %q kept as-is (tool names may differ)", tool, matcher)}
	}
	if names == nil {
		return "", nil
	}
	var plain, mcp []string
	for _, n := range names {
		if strings.HasPrefix(n, "mcp__") {
			mcp = append(mcp, n)
		} else {
			plain = append(plain, n)
		}
	}
	mapped, unknown := mapTools(tool, plain, mappings, defaults)
	mapped = append(mapped, mcp...)
	return strings.Join(mapped, "|"), unknownToolsWarning(tool, unknown)
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlStringList(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = tomlString(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
