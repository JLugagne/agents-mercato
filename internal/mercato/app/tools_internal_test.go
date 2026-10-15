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
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/fsadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/txadapter"
)

func (f *syncFixture) toolApp(t *testing.T, projectDir string, tools ...string) *App {
	t.Helper()
	enabled := map[string]bool{"claude": true}
	for _, tool := range tools {
		enabled[tool] = true
	}
	configPath := filepath.Join(projectDir, ".claude", ".mct.yaml")
	cfgStore := cfgadapter.NewConfigStore()
	require.NoError(t, cfgStore.Save(configPath, domain.Config{
		LocalPath: filepath.Join(projectDir, ".claude"),
		Markets:   []domain.MarketConfig{{Name: marketName, URL: f.sourceDir, Branch: "main"}},
		Tools:     enabled,
	}))
	return New(f.git, fsadapter.New(), cfgStore, cfgadapter.NewStateStore(), cfgadapter.NewInstallDB(),
		configPath, f.cacheDir,
		WithTxManager(txadapter.New(filepath.Join(f.cacheDir, "staging"))),
		WithTransformers(transform.Registry()),
		WithToolMappings(cfgadapter.NewToolMappingStore()))
}

func setTools(t *testing.T, a *App, tools ...string) {
	t.Helper()
	cfg, err := a.cfg.Load(a.configPath)
	require.NoError(t, err)
	cfg.Tools = map[string]bool{"claude": true}
	for _, tool := range tools {
		cfg.Tools[tool] = true
	}
	require.NoError(t, a.cfg.Save(a.configPath, cfg))
}

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
}

func toolLocation(t *testing.T, a *App, profile, projectDir, tool string) *domain.InstalledLocation {
	t.Helper()
	db, err := a.idb.Load(a.cacheDir)
	require.NoError(t, err)
	pkg := db.FindPackage(marketName, profile)
	require.NotNil(t, pkg)
	return findLocationByPathAndType(pkg, projectDir, tool)
}

func hookGroups(t *testing.T, path string) []any {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(b, &doc))
	hooks, _ := doc["hooks"].(map[string]any)
	groups, _ := hooks["PreToolUse"].([]any)
	return groups
}

const skillRef = domain.MctRef(marketName + "@dev/go/skills/go-arch/SKILL.md")

func TestTools_SharedSkillWrittenOnceWithAssets(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	mkdirs(t, p, ".codex", ".gemini")
	a := f.toolApp(t, p, "codex", "gemini")

	_, err := a.Add(skillRef, service.AddOpts{})
	require.NoError(t, err)

	require.Contains(t, readFile(t, filepath.Join(p, ".agents/skills/go-arch/SKILL.md")), "name: go-arch")
	require.Equal(t, "You are a Go architect.\n", readFile(t, filepath.Join(p, ".agents/skills/go-arch/prompt.md")))
	shared := toolLocation(t, a, "dev/go", p, domain.RuntimeTypeSharedAgents)
	require.NotNil(t, shared)
	require.Len(t, shared.Files, 2)
	require.Nil(t, toolLocation(t, a, "dev/go", p, "codex"))
	require.Nil(t, toolLocation(t, a, "dev/go", p, "gemini"))

	_, err = a.Remove(skillRef, service.RemoveOpts{})
	require.NoError(t, err)
	require.NoDirExists(t, filepath.Join(p, ".agents/skills/go-arch"))
}

func TestTools_HookPatchesSharedFileAndRemovesOnlyOwnEntry(t *testing.T) {
	files := marketFiles()
	files["dev/go/hooks/vet.json"] = `{"event":"PreToolUse","matcher":"Bash","hooks":[{"type":"command","command":"go vet ./..."}]}`
	files["dev/go/hooks/lint.json"] = `{"event":"PreToolUse","matcher":"Edit","hooks":[{"type":"command","command":"golangci-lint run"}]}`
	f := newSyncFixture(t, files)
	p := t.TempDir()
	mkdirs(t, p, ".codex")
	hooksJSON := filepath.Join(p, ".codex/hooks.json")
	require.NoError(t, os.WriteFile(hooksJSON, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"user-own"}]}]}}`), 0o644))
	a := f.toolApp(t, p, "codex")
	vet := domain.MctRef(marketName + "@dev/go/hooks/vet.json")
	lint := domain.MctRef(marketName + "@dev/go/hooks/lint.json")

	_, err := a.Add(vet, service.AddOpts{})
	require.NoError(t, err)
	_, err = a.Add(lint, service.AddOpts{})
	require.NoError(t, err)
	require.Len(t, hookGroups(t, hooksJSON), 3)

	statuses, err := a.Check(service.CheckOpts{})
	require.NoError(t, err)
	require.Equal(t, domain.StateClean, findStatus(statuses, vet).ToolStates["codex"])

	_, err = a.Remove(vet, service.RemoveOpts{})
	require.NoError(t, err)
	groups := hookGroups(t, hooksJSON)
	require.Len(t, groups, 2)
	require.Contains(t, readFile(t, hooksJSON), "user-own")
	require.Contains(t, readFile(t, hooksJSON), "golangci-lint run")
	require.NotContains(t, readFile(t, hooksJSON), "go vet")
}

func TestTools_HookPatchRemovedByUserIsToolDrift(t *testing.T) {
	files := marketFiles()
	files["dev/go/hooks/vet.json"] = `{"event":"PreToolUse","matcher":"Bash","hooks":[{"type":"command","command":"go vet ./..."}]}`
	f := newSyncFixture(t, files)
	p := t.TempDir()
	mkdirs(t, p, ".codex")
	a := f.toolApp(t, p, "codex")
	vet := domain.MctRef(marketName + "@dev/go/hooks/vet.json")
	_, err := a.Add(vet, service.AddOpts{})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(p, ".codex/hooks.json"), []byte(`{}`), 0o644))

	statuses, err := a.Check(service.CheckOpts{})
	require.NoError(t, err)
	require.Equal(t, domain.StateDrift, findStatus(statuses, vet).ToolStates["codex"])
}

func TestTools_UpdateRewritesEnabledAndDropsDisabledTool(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	mkdirs(t, p, ".codex", ".gemini")
	a := f.toolApp(t, p, "codex", "gemini")
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(p, ".gemini/agents/code-review.md"))

	setTools(t, a, "codex")
	f.push(t, a, map[string]string{"dev/go/agents/code-review.md": "---\ndescription: \"Go code reviewer\"\n---\nReview Go code, v2.\n"})
	_, err = a.Update(service.UpdateOpts{})
	require.NoError(t, err)

	require.Contains(t, readFile(t, filepath.Join(p, ".codex/agents/code-review.toml")), "v2")
	require.NoFileExists(t, filepath.Join(p, ".gemini/agents/code-review.md"))
	require.Nil(t, toolLocation(t, a, "dev/go", p, "gemini"))
}

func TestTools_UpdateCleansFilesOfRemovedTools(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	a := f.toolApp(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)

	legacy := filepath.Join(p, ".supermavenrules")
	require.NoError(t, os.WriteFile(legacy, []byte("old"), 0o644))
	db, err := a.idb.Load(a.cacheDir)
	require.NoError(t, err)
	pkg := db.FindPackage(marketName, "dev/go")
	pkg.Locations = append(pkg.Locations, domain.InstalledLocation{Path: p, Type: "supermaven", Files: []domain.InstalledFile{{Path: ".supermavenrules", XXH: xxhashHex([]byte("old"))}}})
	require.NoError(t, a.idb.Lock(a.cacheDir))
	require.NoError(t, a.idb.Save(a.cacheDir, db))
	require.NoError(t, a.idb.Unlock(a.cacheDir))

	f.push(t, a, map[string]string{"dev/go/agents/code-review.md": "---\ndescription: \"Go code reviewer\"\n---\nv2\n"})
	_, err = a.Update(service.UpdateOpts{})
	require.NoError(t, err)

	require.NoFileExists(t, legacy)
	require.Nil(t, toolLocation(t, a, "dev/go", p, "supermaven"))
}

func TestTools_UpstreamRemovedEntryPrunesToolFiles(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	mkdirs(t, p, ".codex")
	a := f.toolApp(t, p, "codex")
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(p, ".codex/agents/code-review.toml"))
	require.FileExists(t, filepath.Join(p, ".agents/skills/go-arch/SKILL.md"))

	removeFromRepo(t, f.sourceDir, []string{"dev/go/agents/code-review.md"}, "drop agent")
	_, err = a.Refresh(service.RefreshOpts{})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(p, ".claude/agents/code-review.md"))

	require.NoFileExists(t, filepath.Join(p, ".codex/agents/code-review.toml"))
	require.FileExists(t, filepath.Join(p, ".agents/skills/go-arch/SKILL.md"))
}

func TestTools_UpdateKeepsLocallyModifiedObsoleteToolFile(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	a := f.toolApp(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)

	legacy := filepath.Join(p, ".github/copilot-instructions.md")
	mkdirs(t, p, ".github")
	require.NoError(t, os.WriteFile(legacy, []byte("user edits"), 0o644))
	db, err := a.idb.Load(a.cacheDir)
	require.NoError(t, err)
	pkg := db.FindPackage(marketName, "dev/go")
	pkg.Locations = append(pkg.Locations, domain.InstalledLocation{Path: p, Type: "copilot", Files: []domain.InstalledFile{{Path: ".github/copilot-instructions.md", XXH: xxhashHex([]byte("written by old mct"))}}})
	require.NoError(t, a.idb.Lock(a.cacheDir))
	require.NoError(t, a.idb.Save(a.cacheDir, db))
	require.NoError(t, a.idb.Unlock(a.cacheDir))

	f.push(t, a, map[string]string{"dev/go/agents/code-review.md": "---\ndescription: \"Go code reviewer\"\n---\nv2\n"})
	results, err := a.Update(service.UpdateOpts{})
	require.NoError(t, err)

	require.Equal(t, "user edits", readFile(t, legacy))
	var warnings []string
	for _, r := range results {
		warnings = append(warnings, r.Warnings...)
	}
	require.Contains(t, strings.Join(warnings, "\n"), ".github/copilot-instructions.md")
}
