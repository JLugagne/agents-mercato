package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/fsadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/gitadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/txadapter"
)

type syncFixture struct {
	sourceDir string
	cacheDir  string
	git       *gitadapter.Adapter
}

func newSyncFixture(t *testing.T, repoFiles map[string]string) *syncFixture {
	t.Helper()
	f := &syncFixture{
		sourceDir: createTestRepo(t, repoFiles),
		cacheDir:  t.TempDir(),
		git:       gitadapter.New(gitadapter.WithDepth(0)),
	}
	cloneTestRepo(t, f.sourceDir, filepath.Join(f.cacheDir, marketDirName(marketName)))
	return f
}

func (f *syncFixture) app(t *testing.T, projectDir string) *App {
	t.Helper()
	configPath := filepath.Join(projectDir, ".claude", ".mct.yaml")
	cfgStore := cfgadapter.NewConfigStore()
	require.NoError(t, cfgStore.Save(configPath, domain.Config{
		LocalPath: filepath.Join(projectDir, ".claude"),
		Markets:   []domain.MarketConfig{{Name: marketName, URL: f.sourceDir, Branch: "main"}},
	}))
	return New(f.git, fsadapter.New(), cfgStore, cfgadapter.NewStateStore(), cfgadapter.NewInstallDB(),
		configPath, f.cacheDir, WithTxManager(txadapter.New(filepath.Join(f.cacheDir, "staging"))))
}

func (f *syncFixture) push(t *testing.T, a *App, files map[string]string) {
	t.Helper()
	addCommitToRepo(t, f.sourceDir, files, "upstream change")
	fetchClone(t, a)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func stateOf(t *testing.T, a *App, ref domain.MctRef) domain.EntryState {
	t.Helper()
	statuses, err := a.Check(service.CheckOpts{})
	require.NoError(t, err)
	s := findStatus(statuses, ref)
	require.NotNil(t, s, "ref %s missing from check", ref)
	return s.State
}

const agentRef = domain.MctRef(marketName + "@dev/go/agents/code-review.md")

func TestSync_MultiLocationUpdateDoesNotStaleOtherLocation(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p1, p2 := t.TempDir(), t.TempDir()
	a1, a2 := f.app(t, p1), f.app(t, p2)
	_, err := a1.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	_, err = a2.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)

	f.push(t, a1, map[string]string{"dev/go/agents/code-review.md": "---\ndescription: \"v2\"\n---\nv2 body\n"})

	_, err = a1.Update(service.UpdateOpts{})
	require.NoError(t, err)
	require.Contains(t, readFile(t, filepath.Join(p1, ".claude/agents/code-review.md")), "v2 body")

	require.Equal(t, domain.StateUpdateAvailable, stateOf(t, a2, agentRef))
	_, err = a2.Update(service.UpdateOpts{})
	require.NoError(t, err)
	require.Contains(t, readFile(t, filepath.Join(p2, ".claude/agents/code-review.md")), "v2 body")
}

func TestSync_UnrelatedCommitDoesNotFlagUpdate(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)

	f.push(t, a, map[string]string{"other/profile/agents/x.md": "---\ndescription: \"x\"\n---\nx\n"})

	require.Equal(t, domain.StateClean, stateOf(t, a, agentRef))
}

func TestSync_DryRunDoesNotWrite(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	before := readFile(t, filepath.Join(p, ".claude/agents/code-review.md"))

	f.push(t, a, map[string]string{"dev/go/agents/code-review.md": "---\ndescription: \"v2\"\n---\nv2 body\n"})

	_, err = a.Update(service.UpdateOpts{DryRun: true})
	require.NoError(t, err)
	require.Equal(t, before, readFile(t, filepath.Join(p, ".claude/agents/code-review.md")))
	require.Equal(t, domain.StateUpdateAvailable, stateOf(t, a, agentRef))
}

func TestSync_NestedSkillFileChangeIsApplied(t *testing.T) {
	files := marketFiles()
	files["dev/go/skills/go-arch/references/notes.md"] = "notes v1\n"
	f := newSyncFixture(t, files)
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(domain.MctRef(marketName+"@dev/go/skills/go-arch/SKILL.md"), service.AddOpts{})
	require.NoError(t, err)

	f.push(t, a, map[string]string{"dev/go/skills/go-arch/references/notes.md": "notes v2\n"})

	_, err = a.Update(service.UpdateOpts{})
	require.NoError(t, err)
	require.Equal(t, "notes v2\n", readFile(t, filepath.Join(p, ".claude/skills/go-arch/references/notes.md")))
}

func TestSync_MissingBaseCommitStillUpdates(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)

	idb := cfgadapter.NewInstallDB()
	db, err := idb.Load(f.cacheDir)
	require.NoError(t, err)
	db.FindPackage(marketName, "dev/go").Version = strings.Repeat("d", 40)
	require.NoError(t, idb.Save(f.cacheDir, db))

	f.push(t, a, map[string]string{"dev/go/agents/code-review.md": "---\ndescription: \"v2\"\n---\nv2 body\n"})

	_, err = a.Update(service.UpdateOpts{})
	require.NoError(t, err)
	require.Contains(t, readFile(t, filepath.Join(p, ".claude/agents/code-review.md")), "v2 body")
}

func hookAndAgentFiles(cmd string) map[string]string {
	files := marketFiles()
	files["dev/go/hooks/go-vet.json"] = `{"event":"PreToolUse","matcher":"Bash","hooks":[{"type":"command","command":"` + cmd + `"}]}`
	return files
}

func hookCommands(t *testing.T, settingsPath string) []string {
	t.Helper()
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, settingsPath)), &s))
	var out []string
	for _, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				out = append(out, h.Command)
			}
		}
	}
	return out
}

func TestSync_HookUpstreamChangeIsApplied(t *testing.T) {
	f := newSyncFixture(t, hookAndAgentFiles("go vet v1"))
	p := t.TempDir()
	a := f.app(t, p)
	hookRef := domain.MctRef(marketName + "@dev/go/hooks/go-vet.json")
	_, err := a.Add(hookRef, service.AddOpts{})
	require.NoError(t, err)

	f.push(t, a, map[string]string{"dev/go/hooks/go-vet.json": hookAndAgentFiles("go vet v2")["dev/go/hooks/go-vet.json"]})

	_, err = a.Update(service.UpdateOpts{})
	require.NoError(t, err)
	require.Equal(t, []string{"go vet v2"}, hookCommands(t, filepath.Join(p, ".claude/settings.json")))
	require.Equal(t, domain.StateClean, stateOf(t, a, hookRef))
}

func TestSync_HookAndAgentInSameProfileAreClean(t *testing.T) {
	f := newSyncFixture(t, hookAndAgentFiles("go vet"))
	p := t.TempDir()
	a := f.app(t, p)
	hookRef := domain.MctRef(marketName + "@dev/go/hooks/go-vet.json")
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	_, err = a.Add(hookRef, service.AddOpts{})
	require.NoError(t, err)

	require.Equal(t, domain.StateClean, stateOf(t, a, agentRef))
	require.Equal(t, domain.StateClean, stateOf(t, a, hookRef))
}

func TestSync_AgentWithRequiresSkillsStaysCleanAfterUpdate(t *testing.T) {
	agent := func(body string) string {
		return "---\ndescription: \"reviewer\"\nrequires_skills:\n  - file: dev/go/skills/go-arch/SKILL.md\n---\n" + body + "\n"
	}
	files := marketFiles()
	files["dev/go/agents/code-review.md"] = agent("v1")
	f := newSyncFixture(t, files)
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(agentRef, service.AddOpts{NoDeps: true})
	require.NoError(t, err)
	require.Equal(t, domain.StateClean, stateOf(t, a, agentRef))

	f.push(t, a, map[string]string{"dev/go/agents/code-review.md": agent("v2")})
	_, err = a.Update(service.UpdateOpts{})
	require.NoError(t, err)

	got := readFile(t, filepath.Join(p, ".claude/agents/code-review.md"))
	require.Contains(t, got, "v2")
	require.NotContains(t, got, "requires_skills")
	require.Equal(t, domain.StateClean, stateOf(t, a, agentRef))
}

type flakyFileVersionGit struct{ *gitadapter.Adapter }

func (g flakyFileVersionGit) FileVersion(string, string) (domain.MctVersion, error) {
	return "", errors.New("transient git failure")
}

func TestSync_TransientGitErrorDoesNotPruneInstalledFiles(t *testing.T) {
	f := newSyncFixture(t, hookAndAgentFiles("go vet"))
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)

	a.git = flakyFileVersionGit{f.git}
	pruned, err := a.pruneRemovedUpstreamFiles(marketName, "main", a.clonePath(marketName))
	require.NoError(t, err)
	require.Empty(t, pruned)
	require.FileExists(t, filepath.Join(p, ".claude/agents/code-review.md"))
}

func TestSync_FlatSkillIsNotPrunedOnRefresh(t *testing.T) {
	files := map[string]string{"skills/flat.md": "---\ndescription: \"flat\"\n---\nflat\n"}
	f := newSyncFixture(t, files)
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(domain.MctRef(marketName+"@skills/flat.md"), service.AddOpts{})
	require.NoError(t, err)

	pruned, err := a.pruneRemovedUpstreamFiles(marketName, "main", a.clonePath(marketName))
	require.NoError(t, err)
	require.Empty(t, pruned)
}

func TestPrune_ExistingUpstreamFileIsNotPruned(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)

	results, err := a.Prune(service.PruneOpts{AllKeep: true})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestDoctor_TransientGitErrorIsNotReportedAsRemoved(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)

	a.git = flakyFileVersionGit{f.git}
	report, err := a.Doctor(service.DoctorOpts{})
	require.NoError(t, err)
	require.Empty(t, report.UpstreamRemoved)
}
