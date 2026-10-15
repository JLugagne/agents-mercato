package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
)

const (
	goVetHookRef = domain.MctRef(marketName + "@dev/go/hooks/go-vet.json")
	lintHookRef  = domain.MctRef(marketName + "@dev/go/hooks/lint.json")
	userGroup    = `{"matcher":"Bash","hooks":[{"type":"command","command":"echo user"}]}`
	userLegacy   = `{"type":"command","command":"echo legacy-user","matcher":"Write"}`
)

func twoHookMarketFiles(goVetCmd string) map[string]string {
	files := hookAndAgentFiles(goVetCmd)
	files["dev/go/hooks/lint.json"] = `{"event":"PostToolUse","matcher":"Edit","hooks":[{"type":"command","command":"golangci-lint run","timeout":30}]}`
	return files
}

var (
	allowedGroupKeys   = map[string]bool{"matcher": true, "hooks": true}
	allowedHandlerKeys = map[string]bool{"type": true, "command": true, "timeout": true, "if": true, "statusMessage": true, "async": true, "asyncRewake": true, "shell": true, "args": true}
)

func settingsHooks(t *testing.T, projectDir string) map[string][]map[string]json.RawMessage {
	t.Helper()
	var s struct {
		Hooks map[string][]map[string]json.RawMessage `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(projectDir, ".claude", "settings.json"))), &s))
	return s.Hooks
}

func requireSchemaValidHooks(t *testing.T, hooks map[string][]map[string]json.RawMessage) {
	t.Helper()
	for event, groups := range hooks {
		for _, g := range groups {
			for k := range g {
				require.True(t, allowedGroupKeys[k], "event %s: unknown matcher group key %q", event, k)
			}
			require.Contains(t, g, "hooks", "event %s: matcher group without hooks array", event)
			var handlers []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(g["hooks"], &handlers))
			require.NotEmpty(t, handlers)
			for _, h := range handlers {
				for k := range h {
					require.True(t, allowedHandlerKeys[k], "event %s: unknown hook handler key %q", event, k)
				}
			}
		}
	}
}

func groupCommands(t *testing.T, g map[string]json.RawMessage) (string, []string) {
	t.Helper()
	var matcher string
	if raw, ok := g["matcher"]; ok {
		require.NoError(t, json.Unmarshal(raw, &matcher))
	}
	var handlers []struct {
		Command string `json:"command"`
	}
	require.NoError(t, json.Unmarshal(g["hooks"], &handlers))
	var cmds []string
	for _, h := range handlers {
		cmds = append(cmds, h.Command)
	}
	return matcher, cmds
}

func writeLegacyGoVetState(t *testing.T, f *syncFixture, projectDir string, extra string) {
	t.Helper()
	id := mctIDForRef(goVetHookRef)
	legacy := `{"theme":"dark","hooks":{"PreToolUse":[` + extra + `{"command":"go vet ./...","matcher":"Bash","mct_id":"` + id + `","type":"command"}]}}`
	settings := filepath.Join(projectDir, ".claude", "settings.json")
	require.NoError(t, os.WriteFile(settings, []byte(legacy), 0o644))
	_ = os.Remove(filepath.Join(projectDir, ".claude", "mct-hooks.json"))

	idb := cfgadapter.NewInstallDB()
	db, err := idb.Load(f.cacheDir)
	require.NoError(t, err)
	legacySum := xxhashHex([]byte(`{"command":"go vet ./...","matcher":"Bash","type":"command"}`))
	for mi := range db.Markets {
		for pi := range db.Markets[mi].Packages {
			for li := range db.Markets[mi].Packages[pi].Locations {
				files := db.Markets[mi].Packages[pi].Locations[li].Files
				for fi := range files {
					if strings.HasSuffix(files[fi].Path, "#hooks/go-vet.json") {
						files[fi].XXH = legacySum
					}
				}
			}
		}
	}
	require.NoError(t, idb.Save(f.cacheDir, db))
}

func TestHookFormat_InstallWritesMatcherGroups(t *testing.T) {
	f := newSyncFixture(t, twoHookMarketFiles("go vet ./..."))
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)
	_, err = a.Add(lintHookRef, service.AddOpts{})
	require.NoError(t, err)

	hooks := settingsHooks(t, p)
	requireSchemaValidHooks(t, hooks)
	require.Len(t, hooks["PreToolUse"], 1)
	m, cmds := groupCommands(t, hooks["PreToolUse"][0])
	require.Equal(t, "Bash", m)
	require.Equal(t, []string{"go vet ./..."}, cmds)
	require.Len(t, hooks["PostToolUse"], 1)
	m, cmds = groupCommands(t, hooks["PostToolUse"][0])
	require.Equal(t, "Edit", m)
	require.Equal(t, []string{"golangci-lint run"}, cmds)
	require.NotContains(t, readFile(t, filepath.Join(p, ".claude", "settings.json")), "mct_id")

	require.Equal(t, domain.StateClean, stateOf(t, a, goVetHookRef))
	require.Equal(t, domain.StateClean, stateOf(t, a, lintHookRef))
}

func TestHookFormat_ReinstallDoesNotDuplicateGroup(t *testing.T) {
	f := newSyncFixture(t, twoHookMarketFiles("go vet ./..."))
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)

	w := directWriter{fs: a.fs}
	snippet, err := domain.ParseHookSnippet([]byte(twoHookMarketFiles("go vet ./...")["dev/go/hooks/go-vet.json"]))
	require.NoError(t, err)
	_, err = a.installHookSnippet(w, goVetHookRef, snippet, filepath.Join(p, ".claude", "settings.json"), "go-vet.json")
	require.NoError(t, err)

	require.Len(t, settingsHooks(t, p)["PreToolUse"], 1)
}

func TestHookFormat_LegacyIsCleanBeforeMigration(t *testing.T) {
	f := newSyncFixture(t, twoHookMarketFiles("go vet ./..."))
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)
	writeLegacyGoVetState(t, f, p, "")

	require.Equal(t, domain.StateClean, stateOf(t, a, goVetHookRef))
}

func TestHookFormat_LegacyMigratedOnInstallKeepsUserEntries(t *testing.T) {
	f := newSyncFixture(t, twoHookMarketFiles("go vet ./..."))
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)
	writeLegacyGoVetState(t, f, p, userGroup+","+userLegacy+",")

	_, err = a.Add(lintHookRef, service.AddOpts{})
	require.NoError(t, err)

	raw := readFile(t, filepath.Join(p, ".claude", "settings.json"))
	require.NotContains(t, raw, "mct_id")
	require.Contains(t, raw, `"theme"`)

	hooks := settingsHooks(t, p)
	pre := hooks["PreToolUse"]
	require.Len(t, pre, 3)
	require.JSONEq(t, userGroup, mustMarshal(t, pre[0]))
	require.JSONEq(t, userLegacy, mustMarshal(t, pre[1]))
	m, cmds := groupCommands(t, pre[2])
	require.Equal(t, "Bash", m)
	require.Equal(t, []string{"go vet ./..."}, cmds)
	require.Len(t, hooks["PostToolUse"], 1)

	require.Equal(t, domain.StateClean, stateOf(t, a, goVetHookRef))
	require.Equal(t, domain.StateClean, stateOf(t, a, lintHookRef))
}

func TestHookFormat_LegacyMigratedOnRemove(t *testing.T) {
	f := newSyncFixture(t, twoHookMarketFiles("go vet ./..."))
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)
	_, err = a.Add(lintHookRef, service.AddOpts{})
	require.NoError(t, err)
	id := mctIDForRef(lintHookRef)
	settings := filepath.Join(p, ".claude", "settings.json")
	legacy := `{"hooks":{"PreToolUse":[` + userGroup + `,{"command":"go vet ./...","matcher":"Bash","mct_id":"` + mctIDForRef(goVetHookRef) + `","type":"command"}],"PostToolUse":[{"command":"golangci-lint run","matcher":"Edit","mct_id":"` + id + `","timeout":30,"type":"command"}]}}`
	require.NoError(t, os.WriteFile(settings, []byte(legacy), 0o644))
	_ = os.Remove(filepath.Join(p, ".claude", "mct-hooks.json"))

	_, err = a.Remove(lintHookRef, service.RemoveOpts{})
	require.NoError(t, err)

	hooks := settingsHooks(t, p)
	requireSchemaValidHooks(t, hooks)
	require.NotContains(t, hooks, "PostToolUse")
	require.Len(t, hooks["PreToolUse"], 2)
	require.JSONEq(t, userGroup, mustMarshal(t, hooks["PreToolUse"][0]))

	_, err = a.Remove(goVetHookRef, service.RemoveOpts{})
	require.NoError(t, err)
	hooks = settingsHooks(t, p)
	require.Len(t, hooks["PreToolUse"], 1)
	require.JSONEq(t, userGroup, mustMarshal(t, hooks["PreToolUse"][0]))
}

func TestHookFormat_RemoveDropsOnlyOwnedGroups(t *testing.T) {
	f := newSyncFixture(t, twoHookMarketFiles("go vet ./..."))
	p := t.TempDir()
	settings := filepath.Join(p, ".claude", "settings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(settings), 0o755))
	require.NoError(t, os.WriteFile(settings, []byte(`{"hooks":{"PreToolUse":[`+userGroup+`]}}`), 0o644))
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)
	_, err = a.Add(lintHookRef, service.AddOpts{})
	require.NoError(t, err)
	requireSchemaValidHooks(t, settingsHooks(t, p))

	_, err = a.Remove(goVetHookRef, service.RemoveOpts{})
	require.NoError(t, err)
	_, err = a.Remove(lintHookRef, service.RemoveOpts{})
	require.NoError(t, err)

	hooks := settingsHooks(t, p)
	require.Len(t, hooks, 1)
	require.Len(t, hooks["PreToolUse"], 1)
	require.JSONEq(t, userGroup, mustMarshal(t, hooks["PreToolUse"][0]))
}

func TestHookFormat_ConflictOnSameEventMatcher(t *testing.T) {
	files := twoHookMarketFiles("go vet ./...")
	files["dev/go/hooks/other.json"] = `{"event":"PreToolUse","matcher":"Bash","hooks":[{"type":"command","command":"other"}]}`
	f := newSyncFixture(t, files)
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)
	_, err = a.Add(domain.MctRef(marketName+"@dev/go/hooks/other.json"), service.AddOpts{})
	require.ErrorIs(t, err, domain.ErrConflictHookEventMatcher)
}

func TestHookFormat_LegacyMigratedOnUpdate(t *testing.T) {
	f := newSyncFixture(t, twoHookMarketFiles("go vet ./..."))
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)
	writeLegacyGoVetState(t, f, p, userGroup+",")

	f.push(t, a, map[string]string{"dev/go/hooks/go-vet.json": twoHookMarketFiles("go vet v2")["dev/go/hooks/go-vet.json"]})
	_, err = a.Update(service.UpdateOpts{})
	require.NoError(t, err)

	hooks := settingsHooks(t, p)
	requireSchemaValidHooks(t, hooks)
	require.Len(t, hooks["PreToolUse"], 2)
	require.JSONEq(t, userGroup, mustMarshal(t, hooks["PreToolUse"][0]))
	_, cmds := groupCommands(t, hooks["PreToolUse"][1])
	require.Equal(t, []string{"go vet v2"}, cmds)
	require.Equal(t, domain.StateClean, stateOf(t, a, goVetHookRef))
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
