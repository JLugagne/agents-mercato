package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func TestReadSettings_Empty(t *testing.T) {
	doc, err := readSettings(nil)
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := doc.hooksByEvent()
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 0 {
		t.Errorf("expected empty hooks, got %v", hooks)
	}
}

func TestReadSettings_PreservesSiblings(t *testing.T) {
	src := []byte(`{
		"theme": "dark",
		"keybindings": {"submit": "ctrl+enter"},
		"max_tokens": 4096
	}`)
	doc, err := readSettings(src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := doc.marshal()
	if err != nil {
		t.Fatal(err)
	}
	// All sibling keys must round-trip.
	for _, k := range []string{`"theme"`, `"keybindings"`, `"max_tokens"`, `"dark"`, `"submit"`, `4096`} {
		if !strings.Contains(string(out), k) {
			t.Errorf("output missing %q:\n%s", k, out)
		}
	}
}

func TestHookState_AddAndRemoveGroup(t *testing.T) {
	st, err := parseHookState(nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	snippet, err := domain.ParseHookSnippet([]byte(`{"event":"PreToolUse","matcher":"Bash","hooks":[{"type":"command","command":"go vet ./..."}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ref := domain.MctRef("market@hooks/go-vet.json")
	if _, err := st.add(ref, snippet); err != nil {
		t.Fatal(err)
	}
	if err := st.doc.setHooksByEvent(st.hooks); err != nil {
		t.Fatal(err)
	}
	out, _ := st.doc.marshal()
	if strings.Contains(string(out), "mct_id") {
		t.Errorf("settings must not carry mct_id:\n%s", out)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	groups := parsed.Hooks["PreToolUse"]
	if len(groups) != 1 || groups[0].Matcher != "Bash" || len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Command != "go vet ./..." {
		t.Fatalf("unexpected matcher groups:\n%s", out)
	}

	owners, _ := json.Marshal(st.owners)
	st2, err := parseHookState(out, owners, true)
	if err != nil {
		t.Fatal(err)
	}
	st2.removeOwned(st2.resolve(), mctIDForRef(ref))
	if err := st2.doc.setHooksByEvent(st2.hooks); err != nil {
		t.Fatal(err)
	}
	out2, _ := st2.doc.marshal()
	if strings.Contains(string(out2), `"hooks"`) {
		t.Errorf("expected hooks key dropped after removal:\n%s", out2)
	}
	if len(st2.owners) != 0 {
		t.Errorf("expected owner record dropped, got %v", st2.owners)
	}
}

func TestHookState_Conflicts(t *testing.T) {
	settings := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"x"}]}]}}`)
	owners := []byte(`{"abc":{"event":"PreToolUse","matcher":"Bash","hooks":[{"type":"command","command":"x"}]}}`)
	st, err := parseHookState(settings, owners, true)
	if err != nil {
		t.Fatal(err)
	}
	resolved := st.resolve()
	if !st.conflicts(resolved, "PreToolUse", "Bash", "different-id") {
		t.Error("expected conflict for different mct_id with same event+matcher")
	}
	if st.conflicts(resolved, "PreToolUse", "Bash", "abc") {
		t.Error("same mct_id should not conflict with itself")
	}
	if st.conflicts(resolved, "PreToolUse", "Edit", "different-id") {
		t.Error("different matcher should not conflict")
	}
	if st.conflicts(resolved, "PostToolUse", "Bash", "different-id") {
		t.Error("different event should not conflict")
	}
}

func TestHookState_LegacyConflicts(t *testing.T) {
	st, err := parseHookState([]byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"x","matcher":"Bash","mct_id":"abc"}]}}`), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !st.conflicts(st.resolve(), "PreToolUse", "Bash", "different-id") {
		t.Error("legacy flat mct entry must still block a conflicting install")
	}
}

func TestHookState_DriftedGroupResolvesWithoutTouchingUserGroup(t *testing.T) {
	settings := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"edited"}]}]}}`)
	owners := []byte(`{"abc":{"event":"PreToolUse","matcher":"Bash","hooks":[{"type":"command","command":"x"}]}}`)
	st, err := parseHookState(settings, owners, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.resolve()["abc"]; !ok {
		t.Error("single drifted candidate must resolve to the owner")
	}

	ambiguous := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"user"}]},{"matcher":"Bash","hooks":[{"type":"command","command":"edited"}]}]}}`)
	st, err = parseHookState(ambiguous, owners, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.resolve()["abc"]; ok {
		t.Error("ambiguous drifted candidates must not resolve, to protect user groups")
	}
}

func TestMctIDForRef_Deterministic(t *testing.T) {
	a := mctIDForRef("m@hooks/foo.json")
	b := mctIDForRef("m@hooks/foo.json")
	if a != b {
		t.Errorf("expected determinism: %q vs %q", a, b)
	}
	c := mctIDForRef("m@hooks/bar.json")
	if a == c {
		t.Errorf("different refs should give different ids")
	}
}

func TestCanonicalHookBody_StripsMctID(t *testing.T) {
	body := json.RawMessage(`{"type":"command","mct_id":"abc","command":"x"}`)
	canonical, err := canonicalHookBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canonical), "mct_id") {
		t.Errorf("canonical should not contain mct_id: %s", canonical)
	}
	// Different mct_id but same body should produce same canonical bytes.
	body2 := json.RawMessage(`{"type":"command","mct_id":"xyz","command":"x"}`)
	canonical2, _ := canonicalHookBody(body2)
	if string(canonical) != string(canonical2) {
		t.Errorf("canonical should be stable across mct_id changes:\n%s\nvs\n%s", canonical, canonical2)
	}
}
