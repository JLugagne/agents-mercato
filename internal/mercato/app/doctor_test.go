package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore/configstoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/filesystem/filesystemtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo/gitrepotest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore/statestoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// doctorEnv builds a minimal env where stat/read/git are entirely controlled
// by the test. cfgMarkets is the list of market names that should be in the
// user's config.
func doctorEnv(
	cfgMarkets []string,
	statExisting map[string]bool,
	readResp func(name string) ([]byte, error),
	upstreamMissingSuffix []string,
	upstreamPresentSkillDirs map[string]bool,
) (*configstoretest.MockConfigStore, *statestoretest.MockStateStore, *gitrepotest.MockGitRepo, *filesystemtest.MockFilesystem) {
	mc := make([]domain.MarketConfig, 0, len(cfgMarkets))
	for _, n := range cfgMarkets {
		mc = append(mc, domain.MarketConfig{Name: n, Branch: "main"})
	}
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: mc}, nil
		},
	}
	state := &statestoretest.MockStateStore{}
	git := &gitrepotest.MockGitRepo{
		FileVersionFn: func(clonePath, filePath string) (domain.MctVersion, error) {
			for _, suf := range upstreamMissingSuffix {
				if hasSuffix(filePath, suf) {
					return "", gitrepo.ErrFileNotFound
				}
			}
			return "v", nil
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			for present := range upstreamPresentSkillDirs {
				if hasSuffix(dirPrefix, present) {
					return []string{dirPrefix + "/SKILL.md"}, nil
				}
			}
			return nil, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		StatFn: func(name string) (fs.FileInfo, error) {
			if statExisting[name] {
				return fakeDirInfo{name: name}, nil
			}
			return nil, &fs.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
		},
		ReadFileFn: readResp,
	}
	return cfg, state, git, fsMock
}

// hasSuffix avoids importing strings just for one helper used by closures
// — keeps the test deps tight.
func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// TestDoctor_ReportsAllCategories drives every detector at once: a stale
// location, an orphaned package (market not in cfg), a modified file, a
// locally-deleted file, and an upstream-removed file.
func TestDoctor_ReportsAllCategories(t *testing.T) {
	cfg, state, git, fsMock := doctorEnv(
		[]string{"mkt"},                      // "orphan-mkt" intentionally not in config
		map[string]bool{"/proj/alive": true}, // /proj/dead is stale
		func(name string) ([]byte, error) {
			switch filepath.Base(name) {
			case "modified.md":
				return []byte("changed"), nil
			default:
				return nil, &fs.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
			}
		},
		[]string{"/upstream-gone.md"},
		nil,
	)

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/group",
					Version: "v1",
					Files:   domain.InstalledFiles{Agents: []string{"modified.md", "deleted.md", "upstream-gone.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: "/proj/alive",
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/modified.md", XXH: xxhashHex([]byte("original"))},
								{Path: ".claude/agents/deleted.md", XXH: "h2"},
							},
						},
						{
							Path: "/proj/dead",
							Type: domain.RuntimeTypeClaudeCode,
						},
					},
				},
			}},
			{Market: "orphan-mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "p",
					Version: "v1",
					Files:   domain.InstalledFiles{Agents: []string{"x.md"}},
					Locations: []domain.InstalledLocation{
						{Path: "/proj/alive", Type: domain.RuntimeTypeClaudeCode},
					},
				},
			}},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	report, err := app.Doctor(service.DoctorOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if len(report.StaleLocations) != 1 || report.StaleLocations[0].Location != "/proj/dead" {
		t.Errorf("expected one stale location /proj/dead, got %+v", report.StaleLocations)
	}
	if len(report.OrphanedPackages) != 1 || report.OrphanedPackages[0].Market != "orphan-mkt" {
		t.Errorf("expected one orphan from orphan-mkt, got %+v", report.OrphanedPackages)
	}
	if len(report.ModifiedFiles) != 1 || report.ModifiedFiles[0].Path != ".claude/agents/modified.md" {
		t.Errorf("expected one modified file, got %+v", report.ModifiedFiles)
	}
	if len(report.LocallyDeleted) != 1 || report.LocallyDeleted[0].Path != ".claude/agents/deleted.md" {
		t.Errorf("expected one locally-deleted file, got %+v", report.LocallyDeleted)
	}
	if len(report.UpstreamRemoved) != 1 || report.UpstreamRemoved[0].Path != "agent/upstream-gone.md" {
		t.Errorf("expected one upstream-removed agent, got %+v", report.UpstreamRemoved)
	}
	if !report.HasFindings() {
		t.Error("expected HasFindings()=true")
	}
}

// TestDoctor_CleanReportsNothing verifies the happy path: every file is
// present and matches its recorded hash, no stale locations, no upstream
// removals — report has no findings.
func TestDoctor_CleanReportsNothing(t *testing.T) {
	cfg, state, git, fsMock := doctorEnv(
		[]string{"mkt"},
		map[string]bool{"/proj": true},
		func(name string) ([]byte, error) {
			return []byte("k"), nil
		},
		nil,
		nil,
	)

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/group",
					Version: "v1",
					Files:   domain.InstalledFiles{Agents: []string{"a.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: "/proj",
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/a.md", XXH: xxhashHex([]byte("k"))},
							},
						},
					},
				},
			}},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	report, err := app.Doctor(service.DoctorOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if report.HasFindings() {
		t.Errorf("expected clean report, got %+v", report)
	}
}

// TestDoctor_ReadOnly ensures Doctor never writes anywhere. We make every
// write entry-point fail loudly so any accidental call would panic.
func TestDoctor_ReadOnly(t *testing.T) {
	cfg, state, git, fsMock := doctorEnv(
		[]string{"mkt"},
		map[string]bool{"/proj": true},
		func(name string) ([]byte, error) {
			return nil, &fs.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
		},
		nil,
		nil,
	)
	fsMock.WriteFileFn = func(path string, content []byte) error {
		t.Fatalf("Doctor must not WriteFile, got %s", path)
		return nil
	}
	fsMock.DeleteFileFn = func(path string) error {
		t.Fatalf("Doctor must not DeleteFile, got %s", path)
		return nil
	}
	fsMock.RemoveAllFn = func(path string) error {
		t.Fatalf("Doctor must not RemoveAll, got %s", path)
		return nil
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "p",
					Version: "v1",
					Files:   domain.InstalledFiles{Agents: []string{"a.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: "/proj",
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/a.md", XXH: "anything"},
							},
						},
					},
				},
			}},
		},
	}
	// Save must NOT be called either.
	idb := idbWithData(db)
	idb.SaveFn = func(cacheDir string, _ domain.InstallDatabase) error {
		t.Fatal("Doctor must not call InstallDB.Save")
		return nil
	}

	app := newTestApp(cfg, git, fsMock, state, idb)
	if _, err := app.Doctor(service.DoctorOpts{}); err != nil {
		t.Fatal("unexpected error:", err)
	}
}
