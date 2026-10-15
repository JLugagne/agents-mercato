package domain

import (
	"encoding/json"
	"sort"
	"strings"
)

// RuntimeTypeSharedAgents is the location type that records files written to
// cross-tool locations (.agents/skills/...). Several tools read them, so they
// are written once and owned by no single tool.
const RuntimeTypeSharedAgents = "agents"

// ToolMapping holds model and tool name mappings used during transformation.
// Models maps source model names to per-tool equivalents (e.g. "sonnet" -> {"opencode": "anthropic/claude-sonnet-4-5"}).
// Tools maps source tool names to per-tool equivalents (e.g. "Bash" -> {"gemini": "run_shell_command"}).
// An empty per-tool value means "drop it silently".
type ToolMapping struct {
	Models map[string]map[string]string
	Tools  map[string]map[string]string
}

// TransformAsset is a supporting file of a skill directory. Path is relative
// to the skill directory and never names SKILL.md itself.
type TransformAsset struct {
	Path    string
	Content []byte
}

// TransformInput is everything a Transformer receives for one entry.
// Exists reports whether a project-relative path exists in the target
// project; it is never nil when called by the app.
type TransformInput struct {
	Entry    Entry
	Content  []byte
	Assets   []TransformAsset
	Mappings ToolMapping
	Exists   func(projectRelPath string) bool
}

// OutputFile is a file written verbatim into the project. Path is
// project-relative and slash-separated. Shared files (under .agents/) are
// written once even when several tools emit them.
type OutputFile struct {
	Path    string
	Content []byte
}

// IsShared reports whether the file lives in a cross-tool location.
func (f OutputFile) IsShared() bool {
	return strings.HasPrefix(f.Path, ".agents/")
}

// HookPatch appends Entry to the array found at Keys + Event inside the JSON
// document File, which the user may share with their own settings. Header
// keys are set at the document root when absent (e.g. "version": 1).
type HookPatch struct {
	File   string
	Keys   []string
	Event  string
	Entry  json.RawMessage
	Header map[string]any
}

// Anchor returns the record path identifying the patched array:
// "<file>#<key>/.../<event>".
func (p HookPatch) Anchor() string {
	return p.File + "#" + strings.Join(append(append([]string(nil), p.Keys...), p.Event), "/")
}

// TransformResult holds the output of a single transformer invocation.
type TransformResult struct {
	ToolName   string
	Files      []OutputFile
	Hooks      []HookPatch
	Warnings   []string
	Skipped    bool
	SkipReason string
}

// Transformer converts mct entries (written in Claude Code format) into the
// project-level format of another AI coding tool. Implementations are pure:
// all I/O goes through TransformInput.
type Transformer interface {
	ToolName() string

	// Detect lists project-relative paths whose existence means the tool is
	// used in a project; the app skips the tool when none exists.
	Detect() []string

	Transform(in TransformInput) TransformResult
}

// TransformerRegistry is a named collection of transformers keyed by tool name.
type TransformerRegistry map[string]Transformer

// Get returns the transformer for the given tool name and whether it exists.
func (r TransformerRegistry) Get(name string) (Transformer, bool) {
	t, ok := r[name]
	return t, ok
}

// EnabledTransformers returns, sorted by tool name, the transformers whose
// name maps to true in tools.
func (r TransformerRegistry) EnabledTransformers(tools map[string]bool) []Transformer {
	var result []Transformer
	for name, t := range r {
		if tools[name] {
			result = append(result, t)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ToolName() < result[j].ToolName() })
	return result
}

// Names returns the registered tool names, sorted.
func (r TransformerRegistry) Names() []string {
	names := make([]string, 0, len(r))
	for name := range r {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
