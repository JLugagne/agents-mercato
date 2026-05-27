package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore/configstoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/filesystem/filesystemtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo/gitrepotest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/installdb/installdbtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore/statestoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// agentFile returns minimal valid agent frontmatter content (no mct fields).
func agentFile() []byte {
	return []byte("---\ndescription: test agent\nauthor: alice\n---\n# Agent\nDo stuff.\n")
}

// cfgWithMarket returns a config with one market and a given local path.
func cfgWithMarket(name, url, branch, localPath string) domain.Config {
	return domain.Config{
		LocalPath: localPath,
		Markets: []domain.MarketConfig{
			{Name: name, URL: url, Branch: branch},
		},
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestList_Empty(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{LocalPath: ".claude"}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{})

	entries, err := a.List(service.ListOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(entries))
	}
}

func TestList_InstalledFilter(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
							{
								Profile:   "agents/bar.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"bar.md"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	// Without filter: both entries returned (both installed at ".")
	entries, err := a.List(service.ListOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	// With installed filter: all entries from installdb are installed
	installed, err := a.List(service.ListOpts{Installed: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(installed) != 2 {
		t.Errorf("expected 2 installed entries, got %d", len(installed))
	}
}

// ---------------------------------------------------------------------------
// GetEntry
// ---------------------------------------------------------------------------

func TestGetEntry_Found(t *testing.T) {
	// GetEntry now requires the on-disk target to exist (disk truth beats
	// pkg.Files, which is package-wide and survives RemoveLocation).
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(filepath.Join(localPath, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localPath, "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Dir(localPath)

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", localPath), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: projectPath, Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	entry, err := a.GetEntry("mkt@agents/foo.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Market != "mkt" {
		t.Errorf("expected Market=mkt, got %q", entry.Market)
	}
	if entry.Ref != "mkt@agents/foo.md" {
		t.Errorf("unexpected Ref: %v", entry.Ref)
	}
	if entry.Type != domain.EntryTypeAgent {
		t.Errorf("expected type agent, got %q", entry.Type)
	}
}

func TestGetEntry_NotFound(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{LocalPath: ".claude"}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{})

	_, err := a.GetEntry("mkt@agents/foo.md")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "ENTRY_NOT_FOUND") {
		t.Errorf("expected ENTRY_NOT_FOUND, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Add
// ---------------------------------------------------------------------------

func TestAdd_Success(t *testing.T) {
	writeFileCalled := false

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	var writePaths []string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writePaths = append(writePaths, path)
			if path == ".claude/agents/foo.md" {
				writeFileCalled = true
			}
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return agentFile(), nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
	}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{Markets: []domain.InstalledMarket{}}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !writeFileCalled {
		t.Errorf("expected agent write, got writes: %v", writePaths)
	}
}

func TestAdd_AlreadyInstalled(t *testing.T) {
	// "Already installed" is now decided by disk presence, not pkg.Files —
	// so the on-disk agent must exist for this guard to fire.
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(filepath.Join(localPath, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localPath, "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Dir(localPath)

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", localPath), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	// installdb already has the package at this location
	// refProfile("mkt@agents/foo.md") = "agents/foo.md" (first 2 path segments)
	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: projectPath, Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "ENTRY_ALREADY_INSTALLED") {
		t.Errorf("expected ENTRY_ALREADY_INSTALLED, got %v", err)
	}
}

func TestAdd_DryRun(t *testing.T) {
	writeFileCalled := false

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writeFileCalled = true
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return agentFile(), nil
		},
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writeFileCalled {
		t.Error("expected WriteFile NOT to be called in dry-run mode")
	}
}

func TestAdd_MarketNotFound(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{LocalPath: ".claude"}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{})

	_, err := a.Add("unknown@agents/foo.md", service.AddOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "MARKET_NOT_FOUND") {
		t.Errorf("expected MARKET_NOT_FOUND, got %v", err)
	}
}

func TestAdd_MctFieldsInRepo(t *testing.T) {
	// mct_* fields in repo files are no longer rejected (copy-based install).
	// This test verifies that Add succeeds even if the repo file has mct_* fields.
	contentWithMctFields := []byte("---\nmct_ref: mkt@agents/foo.md\ndescription: test\n---\n# content\n")

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return contentWithMctFields, nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestAdd_WithDependency(t *testing.T) {
	agentContent := []byte("---\ndescription: test agent\nauthor: alice\nrequires_skills:\n  - file: skills/dep.md\n---\n# Agent\nDo stuff.\n")
	skillContent := []byte("---\ndescription: a dep skill\nauthor: alice\n---\n# Skill\n")

	readFileAtRefCalls := 0

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			readFileAtRefCalls++
			if filePath == "agents/foo.md" {
				return agentContent, nil
			}
			if filePath == "skills/dep.md" {
				return skillContent, nil
			}
			return nil, errors.New("unexpected file: " + filePath)
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/SKILL.md"}, nil
		},
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if readFileAtRefCalls < 2 {
		t.Errorf("expected ReadFileAtRef to be called at least twice (agent + skill), got %d", readFileAtRefCalls)
	}
}

// ---------------------------------------------------------------------------
// Add — profile-level refs
// ---------------------------------------------------------------------------

func skillFile() []byte {
	return []byte("---\ndescription: test skill\nauthor: alice\n---\n# Skill\nDo stuff.\n")
}

func TestAdd_ProfileExpand_Success(t *testing.T) {
	writePaths := []string{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writePaths = append(writePaths, path)
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadMarketFilesFn: func(clonePath, branch string) ([]gitrepo.MarketFile, error) {
			return []gitrepo.MarketFile{
				{Path: "dev/go/agents/foo.md", Content: agentFile()},
				{Path: "dev/go/skills/bar.md", Content: skillFile()},
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if filePath == "dev/go/agents/foo.md" {
				return agentFile(), nil
			}
			if filePath == "dev/go/skills/bar.md" {
				return skillFile(), nil
			}
			return nil, errors.New("unexpected file: " + filePath)
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/bar.md"}, nil
		},
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	_, err := a.Add("mkt@dev/go", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Two entry writes (agent + skill) plus one DB save per package install
	// (the install database is staged via the same writer for atomicity).
	var entryWrites int
	for _, p := range writePaths {
		if filepath.Base(p) != "installed.json" {
			entryWrites++
		}
	}
	if entryWrites != 2 {
		t.Errorf("expected 2 entry writes, got %d (all writes: %v)", entryWrites, writePaths)
	}
}

func TestAdd_ProfileExpand_DryRun(t *testing.T) {
	writeFileCalled := false

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writeFileCalled = true
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadMarketFilesFn: func(clonePath, branch string) ([]gitrepo.MarketFile, error) {
			return []gitrepo.MarketFile{
				{Path: "dev/go/agents/foo.md", Content: agentFile()},
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return agentFile(), nil
		},
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	_, err := a.Add("mkt@dev/go", service.AddOpts{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writeFileCalled {
		t.Error("expected WriteFile NOT to be called in dry-run mode")
	}
}

func TestAdd_ProfileExpand_AllAlreadyInstalled(t *testing.T) {
	// "Already installed" is now decided by disk presence, not pkg.Files —
	// so the on-disk agent must exist for addProfile to skip every file.
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(filepath.Join(localPath, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localPath, "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Dir(localPath)

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", localPath), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}
	git := &gitrepotest.MockGitRepo{
		ReadMarketFilesFn: func(clonePath, branch string) ([]gitrepo.MarketFile, error) {
			return []gitrepo.MarketFile{
				{Path: "dev/go/agents/foo.md"},
			}, nil
		},
	}

	// installdb already has the package at this location
	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "dev/go",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: projectPath, Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)

	_, err := a.Add("mkt@dev/go", service.AddOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "ENTRY_ALREADY_INSTALLED") {
		t.Errorf("expected ENTRY_ALREADY_INSTALLED, got %v", err)
	}
}

func TestAdd_ProfileExpand_PartialInstall(t *testing.T) {
	// Disk presence drives "already installed", so the foo.md agent must
	// exist on disk for addProfile to skip it and install only bar.md.
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(filepath.Join(localPath, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localPath, "agents", "foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Dir(localPath)

	writePaths := []string{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", localPath), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writePaths = append(writePaths, path)
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadMarketFilesFn: func(clonePath, branch string) ([]gitrepo.MarketFile, error) {
			return []gitrepo.MarketFile{
				{Path: "dev/go/agents/foo.md"}, // already installed
				{Path: "dev/go/skills/bar.md"}, // not installed
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return skillFile(), nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/bar.md"}, nil
		},
	}

	// installdb has foo.md already installed but not bar.md
	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "dev/go",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: projectPath, Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)

	_, err := a.Add("mkt@dev/go", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the bar skill should be written as an entry. The install
	// database is staged via the same writer (counted separately).
	var entryWrites int
	for _, p := range writePaths {
		if filepath.Base(p) != "installed.json" {
			entryWrites++
		}
	}
	if entryWrites != 1 {
		t.Errorf("expected 1 entry write, got %d (all writes: %v)", entryWrites, writePaths)
	}
}

func TestAdd_ProfileExpand_MarketNotFound(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{LocalPath: ".claude"}, nil
		},
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.Add("unknown@dev/go", service.AddOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "MARKET_NOT_FOUND") {
		t.Errorf("expected MARKET_NOT_FOUND, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Remove
// ---------------------------------------------------------------------------

func TestRemove_Success(t *testing.T) {
	deleteFileCalled := false

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error { return nil }, // installed.json save
		DeleteFileFn: func(path string) error {
			deleteFileCalled = true
			if path != filepath.Join(".claude", "agents", "foo.md") {
				t.Errorf("unexpected delete path: %q", path)
			}
			return nil
		},
	}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	_, err := a.Remove("mkt@agents/foo.md", service.RemoveOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteFileCalled {
		t.Error("expected DeleteFile to be called")
	}
}

func TestRemove_LastLocation_PackageRemoved(t *testing.T) {
	var savedDB domain.InstallDatabase

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn:  func(path string, content []byte) error { return nil },
		DeleteFileFn: func(path string) error { return nil },
	}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error {
			savedDB = db
			return nil
		},
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	_, err := a.Remove("mkt@agents/foo.md", service.RemoveOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After removing the last location, the market should be gone entirely
	if len(savedDB.Markets) != 0 {
		t.Errorf("expected 0 markets after removing last location, got %d", len(savedDB.Markets))
	}
}

func TestRemove_NotInstalled(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{LocalPath: ".claude"}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{})

	_, err := a.Remove("mkt@agents/foo.md", service.RemoveOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "ENTRY_NOT_INSTALLED") {
		t.Errorf("expected ENTRY_NOT_INSTALLED, got %v", err)
	}
}

func TestRemove_NotInstalledAtCurrentLocation(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: "/other/project", Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	_, err := a.Remove("mkt@agents/foo.md", service.RemoveOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "ENTRY_NOT_INSTALLED") {
		t.Errorf("expected ENTRY_NOT_INSTALLED, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ReadEntryContent
// ---------------------------------------------------------------------------

func TestReadEntryContent_Success(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	want := []byte("# agent content\n")
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return want, nil
		},
	}
	a := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	got, err := a.ReadEntryContent("mkt", "agents/foo.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestReadEntryContent_MarketNotFound(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{}, nil
		},
	}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.ReadEntryContent("missing", "agents/foo.md")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "MARKET_NOT_FOUND") {
		t.Errorf("expected MARKET_NOT_FOUND, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prune
// ---------------------------------------------------------------------------

func TestPrune_AllKeep(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}

	git := &gitrepotest.MockGitRepo{
		FileVersionFn: func(clonePath, filePath string) (domain.MctVersion, error) {
			return "", errors.New("file gone from registry")
		},
	}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{}, idb)
	results, err := a.Prune(service.PruneOpts{AllKeep: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "kept" {
		t.Errorf("expected action=kept, got %q", results[0].Action)
	}
}

func TestPrune_AllRemove(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}

	git := &gitrepotest.MockGitRepo{
		FileVersionFn: func(clonePath, filePath string) (domain.MctVersion, error) {
			return "", errors.New("file gone from registry")
		},
	}

	deleteFileCalled := false
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error { return nil },
		DeleteFileFn: func(path string) error {
			deleteFileCalled = true
			return nil
		},
	}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)
	results, err := a.Prune(service.PruneOpts{AllRemove: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "removed" {
		t.Errorf("expected action=removed, got %q", results[0].Action)
	}
	if !deleteFileCalled {
		t.Error("expected DeleteFile to be called")
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestInit_EmptyDir(t *testing.T) {
	saveCalled := false
	var savedCfg domain.Config

	cfg := &configstoretest.MockConfigStore{
		SaveFn: func(path string, c domain.Config) error {
			saveCalled = true
			savedCfg = c
			return nil
		},
	}
	mkdirCalled := false
	fsMock := &filesystemtest.MockFilesystem{
		MkdirAllFn: func(path string) error {
			mkdirCalled = true
			return nil
		},
	}

	gitMock := &gitrepotest.MockGitRepo{
		CloneFn: func(url, path string) error { return nil },
		RemoteHEADFn: func(path, branch string) (string, error) {
			return "abc123", nil
		},
	}
	stateMock := &statestoretest.MockStateStore{
		SetMarketSyncCleanFn: func(cacheDir, name, sha string) error {
			return nil
		},
	}

	a := newTestApp(cfg, gitMock, fsMock, stateMock)
	err := a.Init(service.InitOpts{LocalPath: ".claude"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mkdirCalled {
		t.Error("expected MkdirAll to be called")
	}
	if !saveCalled {
		t.Error("expected Save to be called")
	}
	if savedCfg.LocalPath != ".claude" {
		t.Errorf("expected LocalPath=.claude, got %q", savedCfg.LocalPath)
	}
	if len(savedCfg.Markets) == 0 {
		t.Error("expected default markets to be populated")
	}
}

func TestInit_AlreadyInitialized(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		ExistsFn: func(path string) bool { return true },
	}
	fsMock := &filesystemtest.MockFilesystem{}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{})

	err := a.Init(service.InitOpts{LocalPath: ".claude"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) || de.Code != "ALREADY_INITIALIZED" {
		t.Errorf("expected ALREADY_INITIALIZED, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// inferEntryType
// ---------------------------------------------------------------------------

func TestInferEntryType(t *testing.T) {
	cases := []struct {
		relPath  string
		expected domain.EntryType
	}{
		{"dev/go/agents/foo.md", domain.EntryTypeAgent},
		{"dev/go/skills/bar.md", domain.EntryTypeSkill},
		{"dev/go/README.md", ""},
		{"agents/foo.md", domain.EntryTypeAgent},
		{"skills/bar.md", domain.EntryTypeSkill},
	}
	for _, tc := range cases {
		t.Run(tc.relPath, func(t *testing.T) {
			got := inferEntryType(tc.relPath)
			if got != tc.expected {
				t.Errorf("inferEntryType(%q) = %q, want %q", tc.relPath, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveLocalPath
// ---------------------------------------------------------------------------

func TestResolveLocalPath(t *testing.T) {
	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})
	cfg := domain.Config{LocalPath: ".claude"}

	t.Run("agent path", func(t *testing.T) {
		got, err := a.resolveLocalPath(cfg, "dev/go/agents/foo.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(".claude", "agents", "foo.md")
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("skill flat file", func(t *testing.T) {
		got, err := a.resolveLocalPath(cfg, "dev/go/skills/bar.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(".claude", "skills", "bar")
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("skill directory path", func(t *testing.T) {
		got, err := a.resolveLocalPath(cfg, "dev/go-hexagonal/skills/go-architect/SKILL.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(".claude", "skills", "go-architect")
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("path traversal", func(t *testing.T) {
		_, err := a.resolveLocalPath(cfg, "../etc/passwd")
		if err == nil {
			t.Fatal("expected error for path traversal, got nil")
		}
		if !isDomainErrorWithCode(err, "UNSAFE_PATH") {
			t.Errorf("expected UNSAFE_PATH error, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// isSkillDirRef
// ---------------------------------------------------------------------------

func TestIsSkillDirRef(t *testing.T) {
	cases := []struct {
		relPath  string
		expected bool
	}{
		{"skills/azure-ai", true},
		{"skills/go-architect", true},
		{"skills/azure-ai/SKILL.md", false},
		{"skills/bar.md", false},
		{"dev/go/skills/bar", true},
		{"dev/go/skills/bar/SKILL.md", false},
		{"agents/foo.md", false},
		{"agents/foo", false},       // agents dir, not skill
		{"dev/go-hexagonal", false}, // profile, no skills segment
	}
	for _, tc := range cases {
		t.Run(tc.relPath, func(t *testing.T) {
			got := isSkillDirRef(tc.relPath)
			if got != tc.expected {
				t.Errorf("isSkillDirRef(%q) = %v, want %v", tc.relPath, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Add — skill directory ref normalization
// ---------------------------------------------------------------------------

func TestAdd_SkillDirRef(t *testing.T) {
	writeFileCalled := false

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			if path == filepath.Join(".claude/skills/go-arch", "SKILL.md") {
				writeFileCalled = true
			}
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return skillFile(), nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/SKILL.md"}, nil
		},
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	// Pass skill directory ref without /SKILL.md — should be normalized
	_, err := a.Add("mkt@skills/go-arch", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !writeFileCalled {
		t.Error("expected WriteFile to be called")
	}
}

// ---------------------------------------------------------------------------
// Remove — skill directory ref normalization
// ---------------------------------------------------------------------------

func TestRemove_SkillDirRef(t *testing.T) {
	removeAllCalled := false

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		RemoveAllFn: func(path string) error {
			removeAllCalled = true
			expected := filepath.Join(".claude", "skills", "go-arch")
			if path != expected {
				t.Errorf("expected RemoveAll on %q, got %q", expected, path)
			}
			return nil
		},
	}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "skills/go-arch",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Skills: []string{"go-arch"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	// Pass skill directory ref without /SKILL.md — should be normalized
	_, err := a.Remove("mkt@skills/go-arch", service.RemoveOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removeAllCalled {
		t.Error("expected RemoveAll to be called")
	}
}

// ---------------------------------------------------------------------------
// ListSkillDirFiles
// ---------------------------------------------------------------------------

func TestListSkillDirFiles_Success(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{"skills/go-arch/SKILL.md", "skills/go-arch/config.toml", "skills/go-arch/README.md"}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if filePath == "skills/go-arch/SKILL.md" {
				return []byte("# Skill content"), nil
			}
			if filePath == "skills/go-arch/README.md" {
				return []byte("# README"), nil
			}
			return nil, errors.New("unexpected file")
		},
	}
	a := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	result, err := a.ListSkillDirFiles("mkt", "skills/go-arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 files, got %d", len(result))
	}
	// .md files should have content populated
	mdCount := 0
	for _, f := range result {
		if f.Content != "" {
			mdCount++
		}
	}
	if mdCount != 2 {
		t.Errorf("expected 2 .md files with content, got %d", mdCount)
	}
	// Non-.md file should have no content
	for _, f := range result {
		if f.Name == "config.toml" && f.Content != "" {
			t.Errorf("expected no content for config.toml, got %q", f.Content)
		}
	}
}

func TestListSkillDirFiles_ConfigLoadError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{}, errors.New("config broken")
		},
	}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.ListSkillDirFiles("mkt", "skills/go-arch")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListSkillDirFiles_MarketNotFound(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{}, nil
		},
	}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.ListSkillDirFiles("unknown", "skills/go-arch")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isDomainErrorWithCode(err, "MARKET_NOT_FOUND") {
		t.Errorf("expected MARKET_NOT_FOUND, got %v", err)
	}
}

func TestListSkillDirFiles_ListDirFilesError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return nil, errors.New("git list error")
		},
	}
	a := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.ListSkillDirFiles("mkt", "skills/go-arch")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListSkillDirFiles_ReadFileAtRefError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{"skills/go-arch/SKILL.md"}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return nil, errors.New("read error")
		},
	}
	a := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	result, err := a.ListSkillDirFiles("mkt", "skills/go-arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Graceful degradation: file is still listed, but content is empty
	if len(result) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result))
	}
	if result[0].Content != "" {
		t.Errorf("expected empty content on read error, got %q", result[0].Content)
	}
}

// ---------------------------------------------------------------------------
// Init — additional branches
// ---------------------------------------------------------------------------

func TestInit_MkdirAllFailure(t *testing.T) {
	fsMock := &filesystemtest.MockFilesystem{
		MkdirAllFn: func(path string) error {
			return errors.New("mkdir failed")
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{})

	err := a.Init(service.InitOpts{LocalPath: ".claude"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestInit_CloneFailure(t *testing.T) {
	fsMock := &filesystemtest.MockFilesystem{
		MkdirAllFn: func(path string) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		CloneFn: func(url, clonePath string) error {
			return errors.New("clone failed")
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	err := a.Init(service.InitOpts{
		LocalPath: ".claude",
		Markets:   []string{"https://github.com/org/repo"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestInit_RemoteHEADFailure(t *testing.T) {
	fsMock := &filesystemtest.MockFilesystem{
		MkdirAllFn: func(path string) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		CloneFn: func(url, clonePath string) error { return nil },
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "", errors.New("remote HEAD failed")
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	err := a.Init(service.InitOpts{
		LocalPath: ".claude",
		Markets:   []string{"https://github.com/org/repo"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestInit_SetMarketSyncCleanFailure(t *testing.T) {
	fsMock := &filesystemtest.MockFilesystem{
		MkdirAllFn: func(path string) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		CloneFn:      func(url, clonePath string) error { return nil },
		RemoteHEADFn: func(clonePath, branch string) (string, error) { return "abc123", nil },
	}
	state := &statestoretest.MockStateStore{
		SetMarketSyncCleanFn: func(cacheDir, market, newSHA string) error {
			return errors.New("state save failed")
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, state)

	err := a.Init(service.InitOpts{
		LocalPath: ".claude",
		Markets:   []string{"https://github.com/org/repo"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestInit_ConfigSaveFailure(t *testing.T) {
	fsMock := &filesystemtest.MockFilesystem{
		MkdirAllFn: func(path string) error { return nil },
	}
	cfg := &configstoretest.MockConfigStore{
		SaveFn: func(path string, c domain.Config) error {
			return errors.New("save failed")
		},
	}
	gitMock := &gitrepotest.MockGitRepo{
		CloneFn: func(url, path string) error { return nil },
		RemoteHEADFn: func(path, branch string) (string, error) {
			return "abc123", nil
		},
	}
	stateMock := &statestoretest.MockStateStore{
		SetMarketSyncCleanFn: func(cacheDir, name, sha string) error {
			return nil
		},
	}
	a := newTestApp(cfg, gitMock, fsMock, stateMock)

	err := a.Init(service.InitOpts{LocalPath: ".claude"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestInit_EmptyMarkets(t *testing.T) {
	saveCalled := false
	fsMock := &filesystemtest.MockFilesystem{
		MkdirAllFn: func(path string) error { return nil },
	}
	cfg := &configstoretest.MockConfigStore{
		SaveFn: func(path string, c domain.Config) error {
			saveCalled = true
			if len(c.Markets) != 0 {
				t.Errorf("expected empty markets, got %d", len(c.Markets))
			}
			return nil
		},
	}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{})

	err := a.Init(service.InitOpts{LocalPath: ".claude", Markets: []string{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !saveCalled {
		t.Error("expected Save to be called")
	}
}

func TestInit_DefaultLocalPath(t *testing.T) {
	var savedCfg domain.Config
	fsMock := &filesystemtest.MockFilesystem{
		MkdirAllFn: func(path string) error { return nil },
	}
	cfg := &configstoretest.MockConfigStore{
		SaveFn: func(path string, c domain.Config) error {
			savedCfg = c
			return nil
		},
	}
	gitMock := &gitrepotest.MockGitRepo{
		CloneFn: func(url, path string) error { return nil },
		RemoteHEADFn: func(path, branch string) (string, error) {
			return "abc123", nil
		},
	}
	stateMock := &statestoretest.MockStateStore{
		SetMarketSyncCleanFn: func(cacheDir, name, sha string) error {
			return nil
		},
	}
	a := newTestApp(cfg, gitMock, fsMock, stateMock)

	err := a.Init(service.InitOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedCfg.LocalPath != "." {
		t.Errorf("expected default LocalPath='.', got %q", savedCfg.LocalPath)
	}
}

// ---------------------------------------------------------------------------
// Add — additional branches
// ---------------------------------------------------------------------------

func TestAdd_InvalidRefFormat(t *testing.T) {
	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.Add("invalid-no-at-sign", service.AddOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAdd_ConfigLoadFailure(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{}, errors.New("config broken")
		},
	}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAdd_ReadFileAtRefFailure(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return nil, errors.New("file not found in repo")
		},
	}
	a := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAdd_WriteFileFailure(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			return errors.New("write failed")
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return agentFile(), nil
		},
	}
	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Prune — additional branches
// ---------------------------------------------------------------------------

func TestPrune_ConfigLoadFailure(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{}, errors.New("config broken")
		},
	}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.Prune(service.PruneOpts{AllRemove: true})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPrune_FileStillExistsInMarket(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}

	git := &gitrepotest.MockGitRepo{
		FileVersionFn: func(clonePath, filePath string) (domain.MctVersion, error) {
			return "v1", nil // file still exists
		},
	}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{
								Profile:   "agents/foo.md",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Agents: []string{"foo.md"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{}, idb)
	results, err := a.Prune(service.PruneOpts{AllRemove: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results when file still exists, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// List — additional branches
// ---------------------------------------------------------------------------

func TestList_ConfigLoadFailure(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{}, errors.New("config broken")
		},
	}
	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	_, err := a.List(service.ListOpts{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestList_FilterByMarket(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets: []domain.MarketConfig{
					{Name: "mkt1", Branch: "main"},
					{Name: "mkt2", Branch: "main"},
				},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt1",
						Packages: []domain.InstalledPackage{
							{Profile: "agents/foo.md", Version: "abc", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}, Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}}},
						},
					},
					{
						Market: "mkt2",
						Packages: []domain.InstalledPackage{
							{Profile: "agents/bar.md", Version: "abc", Files: domain.InstalledFiles{Agents: []string{"bar.md"}}, Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}}},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	entries, err := a.List(service.ListOpts{Market: "mkt1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for mkt1, got %d", len(entries))
	}
	if entries[0].Market != "mkt1" {
		t.Errorf("expected market=mkt1, got %q", entries[0].Market)
	}
}

func TestList_FilterByType(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{
				Markets: []domain.InstalledMarket{
					{
						Market: "mkt",
						Packages: []domain.InstalledPackage{
							{Profile: "agents/foo.md", Version: "abc", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}, Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}}},
							{Profile: "skills/bar", Version: "abc", Files: domain.InstalledFiles{Skills: []string{"bar"}}, Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}}},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, idb)

	agents, err := a.List(service.ListOpts{Type: domain.EntryTypeAgent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range agents {
		if e.Type != domain.EntryTypeAgent {
			t.Errorf("expected all entries to be agents, got %q", e.Type)
		}
	}

	skills, err := a.List(service.ListOpts{Type: domain.EntryTypeSkill})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range skills {
		if e.Type != domain.EntryTypeSkill {
			t.Errorf("expected all entries to be skills, got %q", e.Type)
		}
	}
}

// ---------------------------------------------------------------------------
// refProfile
// ---------------------------------------------------------------------------

func TestRefProfile_InvalidRef(t *testing.T) {
	ref := domain.MctRef("invalid-no-at-sign")
	result := refProfile(ref)
	if result != "invalid-no-at-sign" {
		t.Errorf("expected raw ref string on parse error, got %q", result)
	}
}

func TestRefProfile_LessThanTwoSegments(t *testing.T) {
	ref := domain.MctRef("mkt@foo.md")
	result := refProfile(ref)
	if result != "" {
		t.Errorf("expected empty string for single segment, got %q", result)
	}
}

func TestRefProfile_TwoOrMoreSegments(t *testing.T) {
	ref := domain.MctRef("mkt@dev/go/agents/foo.md")
	result := refProfile(ref)
	if result != "dev/go" {
		t.Errorf("expected 'dev/go', got %q", result)
	}
}

// TestAdd_DepResolution_HierarchicalProfile pins the rule that same-market
// requires_skills paths in a hierarchical market are resolved relative to the
// caller's profile, not the repo root. Regression: paths like
// "skills/foo/SKILL.md" inside an agent at "dev/team/agents/foo.md" must read
// "dev/team/skills/foo/SKILL.md", not "skills/foo/SKILL.md".
// TestAdd_DepResolution_HierarchicalProfile pins the rule that same-market
// requires_skills paths in a hierarchical market are resolved relative to the
// caller's profile, not the repo root. Regression: paths like
// "skills/foo/SKILL.md" inside an agent at "dev/team/agents/foo.md" must read
// "dev/team/skills/foo/SKILL.md", not "skills/foo/SKILL.md".
func TestAdd_DepResolution_HierarchicalProfile(t *testing.T) {
	agentContent := []byte("---\ndescription: test agent\nauthor: alice\nrequires_skills:\n  - file: skills/bar/SKILL.md\n---\n# Agent\n")
	skillContent := []byte("---\ndescription: bar skill\nauthor: alice\n---\n# Skill\n")

	const (
		agentPath = "dev/team/agents/foo.md"
		skillPath = "dev/team/skills/bar/SKILL.md"
	)

	pathsRead := []string{}
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", ".claude"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			pathsRead = append(pathsRead, filePath)
			switch filePath {
			case agentPath:
				return agentContent, nil
			case skillPath:
				return skillContent, nil
			}
			return nil, errors.New("unexpected file: " + filePath)
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) { return "abc123", nil },
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/SKILL.md"}, nil
		},
		ReadMarketFilesFn: func(clonePath, branch string) ([]gitrepo.MarketFile, error) {
			return []gitrepo.MarketFile{
				{Path: agentPath, Content: agentContent, Version: "abc123"},
				{Path: skillPath, Content: skillContent, Version: "abc123"},
			}, nil
		},
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	if _, err := a.Add(domain.MctRef("mkt@"+agentPath), service.AddOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sawAgent, sawSkill := false, false
	for _, p := range pathsRead {
		switch p {
		case agentPath:
			sawAgent = true
		case skillPath:
			sawSkill = true
		}
	}
	if !sawAgent {
		t.Errorf("expected agent path %q to be read, paths read: %v", agentPath, pathsRead)
	}
	if !sawSkill {
		t.Errorf("expected dep to resolve to %q (caller-profile-relative), paths read: %v", skillPath, pathsRead)
	}
}

// TestAdd_DepResolution_FlatSkillsOnly pins that skills_only (flat) markets
// keep dep paths repo-rooted. A skill at "skills/foo/SKILL.md" depending on
// "skills/bar/SKILL.md" must NOT have its caller profile ("skills/foo") spliced in.
func TestAdd_DepResolution_FlatSkillsOnly(t *testing.T) {
	fooContent := []byte("---\ndescription: foo skill\nauthor: alice\nrequires_skills:\n  - file: skills/bar/SKILL.md\n---\n# Foo\n")
	barContent := []byte("---\ndescription: bar skill\nauthor: alice\n---\n# Bar\n")

	const (
		fooPath = "skills/foo/SKILL.md"
		barPath = "skills/bar/SKILL.md"
	)

	pathsRead := []string{}
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			c := cfgWithMarket("mkt", "https://example.com", "main", ".claude")
			c.Markets[0].SkillsOnly = true
			return c, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			pathsRead = append(pathsRead, filePath)
			switch filePath {
			case fooPath:
				return fooContent, nil
			case barPath:
				return barContent, nil
			}
			return nil, errors.New("unexpected file: " + filePath)
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) { return "abc123", nil },
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/SKILL.md"}, nil
		},
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{})

	if _, err := a.Add(domain.MctRef("mkt@"+fooPath), service.AddOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sawBar := false
	for _, p := range pathsRead {
		if p == barPath {
			sawBar = true
		}
		// Spliced-profile path (the bug we are guarding against).
		if p == "skills/foo/skills/bar/SKILL.md" {
			t.Fatalf("flat market dep was incorrectly profile-prefixed: paths read: %v", pathsRead)
		}
	}
	if !sawBar {
		t.Errorf("expected dep to resolve to %q (repo-rooted), paths read: %v", barPath, pathsRead)
	}
}

func TestInferEntryType_Command(t *testing.T) {
	cases := []struct {
		relPath  string
		expected domain.EntryType
	}{
		{"commands/foo.md", domain.EntryTypeCommand},
		{"dev/go/commands/foo.md", domain.EntryTypeCommand},
		{"commands/sub/foo.md", domain.EntryTypeCommand},
		{"agents/foo.md", domain.EntryTypeAgent},
		{"skills/bar/SKILL.md", domain.EntryTypeSkill},
		{"docs/notes.md", ""},
	}
	for _, tc := range cases {
		t.Run(tc.relPath, func(t *testing.T) {
			got := inferEntryType(tc.relPath)
			if got != tc.expected {
				t.Errorf("inferEntryType(%q) = %q, want %q", tc.relPath, got, tc.expected)
			}
		})
	}
}

func TestResolveLocalPath_Command(t *testing.T) {
	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})
	cfg := domain.Config{LocalPath: ".claude"}

	t.Run("flat command", func(t *testing.T) {
		got, err := a.resolveLocalPath(cfg, "commands/foo.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(".claude", "commands", "foo.md")
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("hierarchical command", func(t *testing.T) {
		got, err := a.resolveLocalPath(cfg, "dev/go/commands/foo.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(".claude", "commands", "foo.md")
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("traversal blocked", func(t *testing.T) {
		_, err := a.resolveLocalPath(cfg, "../commands/foo.md")
		if err == nil {
			t.Fatal("expected UNSAFE_PATH error")
		}
		if !isDomainErrorWithCode(err, "UNSAFE_PATH") {
			t.Errorf("expected UNSAFE_PATH error, got %v", err)
		}
	})
}

func TestAdd_ProfileExpand_SkipsSkillSubFiles(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		t.Fatal(err)
	}

	writePaths := []string{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return cfgWithMarket("mkt", "https://example.com", "main", localPath), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writePaths = append(writePaths, path)
			return nil
		},
		MkdirAllFn: func(path string) error { return nil },
	}

	// Reference file has no frontmatter (starts with "# Heading"), which
	// would cause ParseFrontmatter to fail if processed individually.
	noFmFile := []byte("# Working on a task\n\nSome content.\n")

	git := &gitrepotest.MockGitRepo{
		ReadMarketFilesFn: func(clonePath, branch string) ([]gitrepo.MarketFile, error) {
			return []gitrepo.MarketFile{
				{Path: "dev/kanban/skills/kanban/SKILL.md", Content: skillFile()},
				{Path: "dev/kanban/skills/kanban/references/working-on-task.md", Content: noFmFile},
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if filePath == "dev/kanban/skills/kanban/SKILL.md" {
				return skillFile(), nil
			}
			return noFmFile, nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{
				dirPrefix + "/SKILL.md",
				dirPrefix + "/references/working-on-task.md",
			}, nil
		},
	}

	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)

	_, err := a.Add("mkt@dev/kanban", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
