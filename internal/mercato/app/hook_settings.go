package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// mctIDForRef returns the deterministic mct hook identifier for a ref.
// The ID is the xxhash64 hex of the ref string. It MUST be deterministic so
// `mct restore` regenerates the same ID and `mct remove` can later target
// the right entries.
func mctIDForRef(ref domain.MctRef) string {
	return strconv.FormatUint(xxhash.Sum64String(string(ref)), 16)
}

// canonicalHookBody returns the JSON-marshalled object with the mct_id key
// removed and all keys sorted alphabetically. This is the form hashed for
// drift detection so that user reformatting of settings.json (key reorder,
// whitespace) does not register as drift.
func canonicalHookBody(body json.RawMessage) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var m map[string]interface{}
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	delete(m, "mct_id")
	return marshalCanonical(m)
}

// marshalCanonical marshals v with sorted keys at every nesting level.
// Used for stable hashing.
func marshalCanonical(v interface{}) ([]byte, error) {
	switch x := v.(type) {
	case map[string]interface{}:
		var buf bytes.Buffer
		buf.WriteByte('{')
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := marshalCanonical(x[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []interface{}:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			eb, err := marshalCanonical(e)
			if err != nil {
				return nil, err
			}
			buf.Write(eb)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(v)
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// settingsDoc is the in-memory representation of a project .claude/settings.json
// while we mutate the hooks key. All other top-level keys are preserved
// verbatim via json.RawMessage so unknown configuration is round-tripped
// without modification.
type settingsDoc struct {
	top map[string]json.RawMessage
}

// readSettings reads the file at path and returns a settingsDoc. If the file
// does not exist, returns an empty doc. If the file exists but is malformed,
// returns the error.
func readSettings(content []byte) (settingsDoc, error) {
	doc := settingsDoc{top: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(content)) == 0 {
		return doc, nil
	}
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.UseNumber()
	if err := dec.Decode(&doc.top); err != nil {
		return doc, fmt.Errorf("settings.json: %w", err)
	}
	return doc, nil
}

// hooksByEvent decodes the existing "hooks" key into event → []rawObject,
// or returns an empty map if the key is absent. Returns
// ErrSettingsHooksMalformed when the key exists but has unexpected shape.
func (d settingsDoc) hooksByEvent() (map[string][]json.RawMessage, error) {
	out := map[string][]json.RawMessage{}
	raw, ok := d.top["hooks"]
	if !ok || len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, domain.ErrSettingsHooksMalformed.Wrap(err)
	}
	return out, nil
}

// setHooksByEvent re-marshals the hooks map back into the doc. If the map is
// empty the "hooks" key is dropped from the doc entirely.
func (d *settingsDoc) setHooksByEvent(hooks map[string][]json.RawMessage) error {
	// Drop empty event arrays so we don't leave orphan keys.
	for event, list := range hooks {
		if len(list) == 0 {
			delete(hooks, event)
		}
	}
	if len(hooks) == 0 {
		delete(d.top, "hooks")
		return nil
	}
	raw, err := marshalSortedMap(hooks)
	if err != nil {
		return err
	}
	d.top["hooks"] = raw
	return nil
}

// marshal serialises the doc preserving sibling keys but with sorted top-level
// keys. Indentation matches what mct would produce for any other JSON file:
// 2-space indent, newline-terminated.
func (d settingsDoc) marshal() ([]byte, error) {
	if len(d.top) == 0 {
		return []byte("{}\n"), nil
	}
	keys := make([]string, 0, len(d.top))
	for k := range d.top {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		if i > 0 {
			buf.WriteString(",\n")
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.WriteString("  ")
		buf.Write(kb)
		buf.WriteString(": ")
		// Pretty-print the raw message for readability.
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, d.top[k], "  ", "  "); err != nil {
			// Fall back to writing the raw bytes if it isn't valid JSON
			// (shouldn't happen because it round-tripped through Decode).
			buf.Write(d.top[k])
		} else {
			buf.Write(pretty.Bytes())
		}
	}
	buf.WriteString("\n}\n")
	return buf.Bytes(), nil
}

func marshalSortedMap(m map[string][]json.RawMessage) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		arr, err := json.Marshal(m[k])
		if err != nil {
			return nil, err
		}
		buf.Write(arr)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshalSortedObject(obj map[string]json.RawMessage) ([]byte, error) {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(obj[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// projectRelFromClaude turns an absolute path under a project's .claude/
// tree into the slash-separated path rooted at the project (e.g.
// ".claude/settings.json"). When the path is not under a .claude segment
// the input is returned in slash form.
func projectRelFromClaude(abs string) string {
	root := filepath.Dir(filepath.Clean(abs))
	for {
		if filepath.Base(root) == ".claude" {
			root = filepath.Dir(root)
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// hookFileRepoPathStandalone is a non-method version of (*App).hookFileRepoPath
// for use from helpers (drift detection, sync) that don't carry an *App.
func hookFileRepoPathStandalone(profile, file string) string {
	if profile == "hooks/"+file || strings.HasSuffix(profile, "/hooks/"+file) {
		return profile
	}
	if profile != "" {
		return profile + "/hooks/" + file
	}
	return "hooks/" + file
}
