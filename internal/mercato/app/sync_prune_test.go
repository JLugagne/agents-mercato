package app

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore/configstoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/filesystem/filesystemtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo/gitrepotest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/installdb/installdbtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore/statestoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// fakeDirInfo satisfies fs.FileInfo for a directory.
type fakeDirInfo struct{ name string }

func (f fakeDirInfo) Name() string       { return f.name }
func (f fakeDirInfo) Size() int64        { return 0 }
func (f fakeDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (f fakeDirInfo) ModTime() time.Time { return time.Time{} }
func (f fakeDirInfo) IsDir() bool        { return true }
func (f fakeDirInfo) Sys() any           { return nil }

// statBackedByExisting returns a StatFn that reports IsNotExist for any path
// not in the existing set, and a directory FileInfo otherwise.
func statBackedByExisting(existing map[string]bool) func(name string) (fs.FileInfo, error) {
	return func(name string) (fs.FileInfo, error) {
		if existing[name] {
			return fakeDirInfo{name: name}, nil
		}
		return nil, &fs.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
}

// installDBState backs the install DB through a tx-aware writer. The current
// state lives in `current`; Load returns it. The DB path is "installed.json"
// inside cacheDir, and a WriteFile to that path (which is what a tx commit
// produces via stageDBSave) is decoded back into `current` and `last`.
type installDBState struct {
	mu      sync.Mutex
	path    string
	current domain.InstallDatabase
	last    domain.InstallDatabase
	writes  int
}

func newInstallDBState(initial domain.InstallDatabase, cacheDir string) *installDBState {
	return &installDBState{
		path:    cacheDir + "/installed.json",
		current: initial,
	}
}

func (s *installDBState) idb() *installdbtest.MockInstallDB {
	return &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.current, nil
		},
		PathFn: func(cacheDir string) string { return s.path },
	}
}

// interceptWrite returns a WriteFileFn wrapper that decodes installed.json
// writes back into the state and forwards other paths to inner.
func (s *installDBState) interceptWrite(inner func(path string, content []byte) error) func(path string, content []byte) error {
	return func(path string, content []byte) error {
		if path == s.path {
			s.mu.Lock()
			var db domain.InstallDatabase
			if err := json.Unmarshal(content, &db); err != nil {
				s.mu.Unlock()
				return err
			}
			s.current = db
			s.last = db
			s.writes++
			s.mu.Unlock()
			return nil
		}
		if inner != nil {
			return inner(path, content)
		}
		return nil
	}
}

// TestRefresh_PrunesStaleLocations verifies that a Location pointing to a
// directory that no longer exists is dropped from the install DB during
// Refresh, surfaced in PrunedLocations, and that the surviving Location is
// preserved.
func TestRefresh_PrunesStaleLocations(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{}}, nil
		},
		SetMarketSyncDirtyFn: func(cacheDir, market string) error { return nil },
		SetMarketSyncCleanFn: func(cacheDir, market, newSHA string) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		FetchFn:           func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) { return nil, nil },
		// All upstream files still exist — we are only testing stale-location pruning here.
		FileVersionFn: func(clonePath, filePath string) (domain.MctVersion, error) {
			return domain.MctVersion("v"), nil
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/a.md",
					Version: "v1",
					Files:   domain.InstalledFiles{Agents: []string{"a.md"}},
					Locations: []domain.InstalledLocation{
						{Path: "/proj/alive", Type: domain.RuntimeTypeClaudeCode},
						{Path: "/proj/dead", Type: domain.RuntimeTypeClaudeCode},
					},
				},
			}},
		},
	}
	st := newInstallDBState(db, "/cache/dir")

	fsMock := &filesystemtest.MockFilesystem{
		StatFn: statBackedByExisting(map[string]bool{"/proj/alive": true}),
	}
	fsMock.WriteFileFn = st.interceptWrite(nil)

	app := newTestApp(cfg, git, fsMock, state, st.idb())
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	// The pruned location should appear in some result's PrunedLocations.
	var pruned []string
	for _, r := range results {
		pruned = append(pruned, r.PrunedLocations...)
	}
	if len(pruned) != 1 || pruned[0] != "mkt@agents/a.md -> /proj/dead" {
		t.Fatalf("expected exactly one pruned location for /proj/dead, got %v", pruned)
	}

	// The DB save must have kept /proj/alive and dropped /proj/dead.
	pkg := st.last.FindPackage("mkt", "agents/a.md")
	if pkg == nil {
		t.Fatal("package missing from saved DB")
	}
	if len(pkg.Locations) != 1 || pkg.Locations[0].Path != "/proj/alive" {
		t.Fatalf("expected only /proj/alive to remain, got %+v", pkg.Locations)
	}
}

// TestRefresh_PrunesRemovedUpstreamFiles verifies that an agent which has
// been removed upstream is deleted on disk via the filesystem adapter,
// dropped from pkg.Files, and surfaced in PrunedFiles. A surviving agent in
// the same package must be preserved.
func TestRefresh_PrunesRemovedUpstreamFiles(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{}}, nil
		},
		SetMarketSyncDirtyFn: func(cacheDir, market string) error { return nil },
		SetMarketSyncCleanFn: func(cacheDir, market, newSHA string) error { return nil },
	}

	git := &gitrepotest.MockGitRepo{
		FetchFn:           func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) { return nil, nil },
		// "kept.md" still exists upstream, "gone.md" does not.
		FileVersionFn: func(clonePath, filePath string) (domain.MctVersion, error) {
			if strings.HasSuffix(filePath, "/gone.md") || filePath == "gone.md" {
				return "", gitrepo.ErrFileNotFound
			}
			return domain.MctVersion("v"), nil
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/group",
					Version: "v1",
					Files:   domain.InstalledFiles{Agents: []string{"kept.md", "gone.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: "/proj/alive",
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/kept.md", XXH: "h1"},
								{Path: ".claude/agents/gone.md", XXH: "h2"},
							},
						},
					},
				},
			}},
		},
	}
	st := newInstallDBState(db, "/cache/dir")

	var deleted []string
	fsMock := &filesystemtest.MockFilesystem{
		StatFn: statBackedByExisting(map[string]bool{"/proj/alive": true}),
		DeleteFileFn: func(path string) error {
			deleted = append(deleted, path)
			return nil
		},
	}
	fsMock.WriteFileFn = st.interceptWrite(nil)

	app := newTestApp(cfg, git, fsMock, state, st.idb())
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	// PrunedFiles must list the gone agent.
	var pruned []string
	for _, r := range results {
		pruned = append(pruned, r.PrunedFiles...)
	}
	if len(pruned) != 1 || pruned[0] != "mkt@agents/group#agent/gone.md" {
		t.Fatalf("expected single pruned upstream file for gone.md, got %v", pruned)
	}

	// DeleteFile must have been called for the gone agent path.
	wantDel := "/proj/alive/.claude/agents/gone.md"
	found := false
	for _, p := range deleted {
		if p == wantDel {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DeleteFile(%s), got %v", wantDel, deleted)
	}

	// DB must have dropped gone.md from pkg.Files and from loc.Files; kept.md must remain.
	pkg := st.last.FindPackage("mkt", "agents/group")
	if pkg == nil {
		t.Fatal("package missing from saved DB")
	}
	if len(pkg.Files.Agents) != 1 || pkg.Files.Agents[0] != "kept.md" {
		t.Fatalf("expected pkg.Files.Agents=[kept.md], got %v", pkg.Files.Agents)
	}
	if len(pkg.Locations) != 1 {
		t.Fatalf("expected one location, got %d", len(pkg.Locations))
	}
	for _, f := range pkg.Locations[0].Files {
		if f.Path == ".claude/agents/gone.md" {
			t.Fatalf("gone.md still present in loc.Files: %+v", pkg.Locations[0].Files)
		}
	}
}

// TestRefresh_PruneSkippedOnDryRun verifies that DryRun does not invoke
// either pruning path: no DB save, no filesystem deletion, no Stat-driven
// pruned locations in the result.
func TestRefresh_PruneSkippedOnDryRun(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{}}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		FetchFn:           func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) { return nil, nil },
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/a.md",
					Version: "v1",
					Files:   domain.InstalledFiles{Agents: []string{"a.md"}},
					Locations: []domain.InstalledLocation{
						{Path: "/proj/dead", Type: domain.RuntimeTypeClaudeCode},
					},
				},
			}},
		},
	}

	st := newInstallDBState(db, "/cache/dir")

	statCalled := false
	dbWriteCalled := false
	fsMock := &filesystemtest.MockFilesystem{
		FS: fstest.MapFS{},
		StatFn: func(name string) (fs.FileInfo, error) {
			statCalled = true
			return nil, &fs.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
		},
		DeleteFileFn: func(path string) error {
			t.Fatalf("DeleteFile must not be called in DryRun, got %s", path)
			return nil
		},
	}
	fsMock.WriteFileFn = func(path string, content []byte) error {
		if path == st.path {
			dbWriteCalled = true
		}
		return nil
	}

	app := newTestApp(cfg, git, fsMock, state, st.idb())
	results, err := app.Refresh(service.RefreshOpts{DryRun: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	for _, r := range results {
		if len(r.PrunedLocations) > 0 {
			t.Fatalf("expected no PrunedLocations under DryRun, got %v", r.PrunedLocations)
		}
		if len(r.PrunedFiles) > 0 {
			t.Fatalf("expected no PrunedFiles under DryRun, got %v", r.PrunedFiles)
		}
	}
	if dbWriteCalled {
		t.Fatal("install DB write must not be called under DryRun")
	}
	if statCalled {
		t.Fatal("filesystem Stat must not be called under DryRun (pruning skipped entirely)")
	}
}

// pruneTestEnv builds the standard cfg/state/git/fs scaffold for upstream-prune tests.
// projectExists controls which project paths Stat reports as living dirs.
// upstreamMissingSuffix marks any upstream filePath whose tail matches as
// "gone" (FileVersion errors for it). Skill upstream presence is reported via
// upstreamPresentSkillDirs (matched as suffix of the dirPrefix passed to
// ListDirFiles); skills not in the set are treated as gone.
func pruneTestEnv(
	projectExists map[string]bool,
	upstreamMissingSuffix []string,
	upstreamPresentSkillDirs map[string]bool,
) (*configstoretest.MockConfigStore, *statestoretest.MockStateStore, *gitrepotest.MockGitRepo, *filesystemtest.MockFilesystem, *[]string) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{}}, nil
		},
		SetMarketSyncDirtyFn: func(cacheDir, market string) error { return nil },
		SetMarketSyncCleanFn: func(cacheDir, market, newSHA string) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		FetchFn:           func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) { return nil, nil },
		FileVersionFn: func(clonePath, filePath string) (domain.MctVersion, error) {
			for _, suf := range upstreamMissingSuffix {
				if strings.HasSuffix(filePath, suf) {
					return "", gitrepo.ErrFileNotFound
				}
			}
			return domain.MctVersion("v"), nil
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			for present := range upstreamPresentSkillDirs {
				if strings.HasSuffix(dirPrefix, present) {
					return []string{dirPrefix + "/SKILL.md"}, nil
				}
			}
			return nil, nil
		},
	}
	deleted := &[]string{}
	fsMock := &filesystemtest.MockFilesystem{
		StatFn: statBackedByExisting(projectExists),
		DeleteFileFn: func(path string) error {
			*deleted = append(*deleted, path)
			return nil
		},
		RemoveAllFn: func(path string) error {
			*deleted = append(*deleted, path)
			return nil
		},
	}
	return cfg, state, git, fsMock, deleted
}

// TestRefresh_PrunesRemovedUpstreamSkill verifies the skill switch branch:
// a skill whose upstream directory is empty/missing is removed via
// RemoveAll, dropped from pkg.Files.Skills, and any loc.Files entries under
// its tree are removed too.
func TestRefresh_PrunesRemovedUpstreamSkill(t *testing.T) {
	cfg, state, git, fsMock, deleted := pruneTestEnv(
		map[string]bool{"/proj/alive": true},
		[]string{"skills/gone.md"},
		map[string]bool{"skills/kept": true}, // only "kept" still has upstream files
	)

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "",
					Version: "v1",
					Files:   domain.InstalledFiles{Skills: []string{"kept", "gone"}},
					Locations: []domain.InstalledLocation{
						{
							Path: "/proj/alive",
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/skills/kept/SKILL.md", XXH: "h1"},
								{Path: ".claude/skills/gone/SKILL.md", XXH: "h2"},
								{Path: ".claude/skills/gone/extra.md", XXH: "h3"},
							},
						},
					},
				},
			}},
		},
	}
	st := newInstallDBState(db, "/cache/dir")
	fsMock.WriteFileFn = st.interceptWrite(nil)

	app := newTestApp(cfg, git, fsMock, state, st.idb())
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	var pruned []string
	for _, r := range results {
		pruned = append(pruned, r.PrunedFiles...)
	}
	if len(pruned) != 1 || pruned[0] != "mkt@#skill/gone" {
		t.Fatalf("expected one pruned skill 'gone', got %v", pruned)
	}

	wantDel := "/proj/alive/.claude/skills/gone"
	found := false
	for _, p := range *deleted {
		if p == wantDel {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected RemoveAll(%s), got %v", wantDel, *deleted)
	}

	pkg := st.last.FindPackage("mkt", "")
	if pkg == nil {
		t.Fatal("package missing from saved DB")
	}
	if len(pkg.Files.Skills) != 1 || pkg.Files.Skills[0] != "kept" {
		t.Fatalf("expected pkg.Files.Skills=[kept], got %v", pkg.Files.Skills)
	}
	for _, f := range pkg.Locations[0].Files {
		if strings.HasPrefix(f.Path, ".claude/skills/gone/") {
			t.Fatalf("gone skill files still present: %+v", pkg.Locations[0].Files)
		}
	}
	if len(pkg.Locations[0].Files) != 1 || pkg.Locations[0].Files[0].Path != ".claude/skills/kept/SKILL.md" {
		t.Fatalf("expected only kept SKILL.md to remain, got %+v", pkg.Locations[0].Files)
	}
}

// TestRefresh_PrunesRemovedUpstreamCommand verifies the command switch branch.
func TestRefresh_PrunesRemovedUpstreamCommand(t *testing.T) {
	cfg, state, git, fsMock, deleted := pruneTestEnv(
		map[string]bool{"/proj/alive": true},
		[]string{"/gone-cmd.md"},
		nil,
	)

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "tools",
					Version: "v1",
					Files:   domain.InstalledFiles{Commands: []string{"kept-cmd.md", "gone-cmd.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: "/proj/alive",
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/commands/kept-cmd.md", XXH: "h1"},
								{Path: ".claude/commands/gone-cmd.md", XXH: "h2"},
							},
						},
					},
				},
			}},
		},
	}
	st := newInstallDBState(db, "/cache/dir")
	fsMock.WriteFileFn = st.interceptWrite(nil)

	app := newTestApp(cfg, git, fsMock, state, st.idb())
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	var pruned []string
	for _, r := range results {
		pruned = append(pruned, r.PrunedFiles...)
	}
	if len(pruned) != 1 || pruned[0] != "mkt@tools#command/gone-cmd.md" {
		t.Fatalf("expected one pruned command, got %v", pruned)
	}

	wantDel := "/proj/alive/.claude/commands/gone-cmd.md"
	found := false
	for _, p := range *deleted {
		if p == wantDel {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DeleteFile(%s), got %v", wantDel, *deleted)
	}

	pkg := st.last.FindPackage("mkt", "tools")
	if pkg == nil {
		t.Fatal("package missing from saved DB")
	}
	if len(pkg.Files.Commands) != 1 || pkg.Files.Commands[0] != "kept-cmd.md" {
		t.Fatalf("expected pkg.Files.Commands=[kept-cmd.md], got %v", pkg.Files.Commands)
	}
	if len(pkg.Locations[0].Files) != 1 || pkg.Locations[0].Files[0].Path != ".claude/commands/kept-cmd.md" {
		t.Fatalf("expected only kept-cmd.md to remain, got %+v", pkg.Locations[0].Files)
	}
}

// TestRefresh_PrunesRemovedUpstreamHook verifies the hook switch branch:
// the gone hook is dropped from pkg.Files.Hooks and from loc.Files; the
// kept hook entry is preserved. removeHookSnippet is tolerant of missing
// settings.json so we test the DB-side outcome only.
func TestRefresh_PrunesRemovedUpstreamHook(t *testing.T) {
	cfg, state, git, fsMock, _ := pruneTestEnv(
		map[string]bool{"/proj/alive": true},
		[]string{"/gone-hook.json"},
		nil,
	)

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "tools",
					Version: "v1",
					Files:   domain.InstalledFiles{Hooks: []string{"kept-hook.json", "gone-hook.json"}},
					Locations: []domain.InstalledLocation{
						{
							Path: "/proj/alive",
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/settings.json#hooks/kept-hook.json", XXH: "h1"},
								{Path: ".claude/settings.json#hooks/gone-hook.json", XXH: "h2"},
							},
						},
					},
				},
			}},
		},
	}
	st := newInstallDBState(db, "/cache/dir")
	fsMock.WriteFileFn = st.interceptWrite(nil)

	app := newTestApp(cfg, git, fsMock, state, st.idb())
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	var pruned []string
	for _, r := range results {
		pruned = append(pruned, r.PrunedFiles...)
	}
	if len(pruned) != 1 || pruned[0] != "mkt@tools#hook/gone-hook.json" {
		t.Fatalf("expected one pruned hook, got %v", pruned)
	}

	pkg := st.last.FindPackage("mkt", "tools")
	if pkg == nil {
		t.Fatal("package missing from saved DB")
	}
	if len(pkg.Files.Hooks) != 1 || pkg.Files.Hooks[0] != "kept-hook.json" {
		t.Fatalf("expected pkg.Files.Hooks=[kept-hook.json], got %v", pkg.Files.Hooks)
	}
	if len(pkg.Locations[0].Files) != 1 || pkg.Locations[0].Files[0].Path != ".claude/settings.json#hooks/kept-hook.json" {
		t.Fatalf("expected only kept-hook entry to remain, got %+v", pkg.Locations[0].Files)
	}
}

// TestRefresh_PruneCascadesEmptyPackage verifies that when the last file of
// a package is upstream-removed, the package is dropped entirely from the
// install DB (cascade). The package starts with a single agent that gets
// pruned, leaving pkg.Files empty — the cascade should drop the package.
func TestRefresh_PruneCascadesEmptyPackage(t *testing.T) {
	cfg, state, git, fsMock, _ := pruneTestEnv(
		map[string]bool{"/proj/alive": true},
		[]string{"/only.md"}, // the only agent is gone upstream
		nil,
	)

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/group",
					Version: "v1",
					Files:   domain.InstalledFiles{Agents: []string{"only.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: "/proj/alive",
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/only.md", XXH: "h1"},
							},
						},
					},
				},
			}},
		},
	}
	st := newInstallDBState(db, "/cache/dir")
	fsMock.WriteFileFn = st.interceptWrite(nil)

	app := newTestApp(cfg, git, fsMock, state, st.idb())
	if _, err := app.Refresh(service.RefreshOpts{}); err != nil {
		t.Fatal("unexpected error:", err)
	}

	if pkg := st.last.FindPackage("mkt", "agents/group"); pkg != nil {
		t.Fatalf("expected package to be dropped after its only file was pruned, still present: %+v", pkg)
	}
}
