package app

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore/configstoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/filesystem/filesystemtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo/gitrepotest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/installdb/installdbtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore/statestoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// ---------------------------------------------------------------------------
// Fake transformers for testing
// ---------------------------------------------------------------------------

type fakeTransformer struct {
	name           string
	supportsAgents bool
	supportsSkills bool
	extension      string
	dotDir         string
}

func (f *fakeTransformer) ToolName() string { return f.name }

func (f *fakeTransformer) Detect() []string { return []string{f.dotDir} }

func (f *fakeTransformer) supports(t domain.EntryType) bool {
	if t == domain.EntryTypeAgent {
		return f.supportsAgents
	}
	return f.supportsSkills
}

func (f *fakeTransformer) outputPath(entry domain.Entry) string {
	name := strings.TrimSuffix(entry.Filename, ".md")
	if entry.Type == domain.EntryTypeSkill {
		return f.dotDir + "/rules/" + name + f.extension
	}
	return f.dotDir + "/agents/" + name + f.extension
}

func (f *fakeTransformer) Transform(in domain.TransformInput) domain.TransformResult {
	if !f.supports(in.Entry.Type) {
		return domain.TransformResult{
			ToolName:   f.name,
			Skipped:    true,
			SkipReason: f.name + " does not support " + string(in.Entry.Type) + "s",
		}
	}
	return domain.TransformResult{
		ToolName: f.name,
		Files:    []domain.OutputFile{{Path: f.outputPath(in.Entry), Content: append([]byte("/* "+f.name+" */ "), in.Content...)}},
	}
}

// fakeToolMappingStore implements configstore.ToolMappingStore for tests.
type fakeToolMappingStore struct{}

func (f *fakeToolMappingStore) LoadToolMappings(_ string) (domain.ToolMapping, error) {
	return domain.ToolMapping{
		Models: make(map[string]map[string]string),
		Tools:  make(map[string]map[string]string),
	}, nil
}
func (f *fakeToolMappingStore) SaveToolMappings(_ string, _ domain.ToolMapping) error { return nil }
func (f *fakeToolMappingStore) ToolMappingsExist(_ string) bool                       { return false }
func (f *fakeToolMappingStore) DefaultToolMappings() domain.ToolMapping {
	return domain.ToolMapping{
		Models: make(map[string]map[string]string),
		Tools:  make(map[string]map[string]string),
	}
}

// fakeFileInfo implements fs.FileInfo for Stat mock.
type fakeFileInfo struct{ isDir bool }

func (fi fakeFileInfo) Name() string       { return "." }
func (fi fakeFileInfo) Size() int64        { return 0 }
func (fi fakeFileInfo) Mode() fs.FileMode  { return fs.ModeDir }
func (fi fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fi fakeFileInfo) IsDir() bool        { return fi.isDir }
func (fi fakeFileInfo) Sys() interface{}   { return nil }

func cursorTransformer() *fakeTransformer {
	return &fakeTransformer{
		name:           "cursor",
		supportsAgents: false,
		supportsSkills: true,
		extension:      ".mdc",
		dotDir:         ".cursor",
	}
}

func windsurfTransformer() *fakeTransformer {
	return &fakeTransformer{
		name:           "windsurf",
		supportsAgents: false,
		supportsSkills: true,
		extension:      ".md",
		dotDir:         ".windsurf",
	}
}

func newMultiToolApp(
	cfg *configstoretest.MockConfigStore,
	git *gitrepotest.MockGitRepo,
	fsMock *filesystemtest.MockFilesystem,
	state *statestoretest.MockStateStore,
	transformers domain.TransformerRegistry,
	idbOpts ...*installdbtest.MockInstallDB,
) *App {
	var idb *installdbtest.MockInstallDB
	if len(idbOpts) > 0 && idbOpts[0] != nil {
		idb = idbOpts[0]
	} else {
		idb = &installdbtest.MockInstallDB{
			LockFn:   func(cacheDir string) error { return nil },
			UnlockFn: func(cacheDir string) error { return nil },
			LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
				return domain.InstallDatabase{Markets: []domain.InstalledMarket{}}, nil
			},
			SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
		}
	}
	return New(git, fsMock, cfg, state, idb, "/config/path", "/cache/dir",
		WithTransformers(transformers),
		WithToolMappings(&fakeToolMappingStore{}),
	)
}

// ---------------------------------------------------------------------------
// Multi-tool Add: install skill → files written to multiple tool dirs
// ---------------------------------------------------------------------------

func TestAdd_MultiTool_SkillWrittenToMultipleTools(t *testing.T) {
	writePaths := map[string]bool{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true, "windsurf": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writePaths[path] = true
			return nil
		},
		StatFn: func(name string) (fs.FileInfo, error) {
			// All dot-dirs exist
			return fakeFileInfo{isDir: true}, nil
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

	transformers := domain.TransformerRegistry{
		"cursor":   cursorTransformer(),
		"windsurf": windsurfTransformer(),
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers)

	_, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude write (from the normal code path)
	if !writePaths[".claude/skills/bar/SKILL.md"] {
		t.Error("expected Claude file write to .claude/skills/bar/SKILL.md")
	}

	// Check cursor and windsurf files were written
	foundCursor := false
	foundWindsurf := false
	for path := range writePaths {
		if strings.HasPrefix(path, testProjectPath()+"/.cursor/") {
			foundCursor = true
		}
		if strings.HasPrefix(path, testProjectPath()+"/.windsurf/") {
			foundWindsurf = true
		}
	}
	if !foundCursor {
		t.Errorf("expected cursor file write, got paths: %v", writePaths)
	}
	if !foundWindsurf {
		t.Errorf("expected windsurf file write, got paths: %v", writePaths)
	}
}

// ---------------------------------------------------------------------------
// Multi-tool Remove: remove skill → files deleted from all tool dirs
// ---------------------------------------------------------------------------

func TestRemove_MultiTool_DeletesToolFiles(t *testing.T) {
	deletedPaths := map[string]bool{}
	removedPaths := map[string]bool{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		DeleteFileFn: func(path string) error {
			deletedPaths[path] = true
			return nil
		},
		RemoveAllFn: func(path string) error {
			removedPaths[path] = true
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
								Profile:   "skills/bar",
								Version:   "abc123",
								Files:     domain.InstalledFiles{Skills: []string{"bar"}},
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}, {Path: testProjectPath(), Type: "cursor", Files: []domain.InstalledFile{{Path: ".cursor/rules/bar.mdc", XXH: "x", Source: "skills/bar/SKILL.md"}}}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	transformers := domain.TransformerRegistry{
		"cursor": cursorTransformer(),
	}

	a := newMultiToolApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, transformers, idb)

	_, err := a.Remove("mkt@skills/bar/SKILL.md", service.RemoveOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude removal: .claude/skills/bar directory should be removed
	if !removedPaths[".claude/skills/bar"] {
		t.Errorf("expected .claude/skills/bar to be removed, got removed paths: %v", removedPaths)
	}

	// Cursor file removal
	foundCursorDelete := false
	for path := range deletedPaths {
		if strings.Contains(path, ".cursor") {
			foundCursorDelete = true
		}
	}
	if !foundCursorDelete {
		t.Errorf("expected cursor file deletion, got deleted paths: %v", deletedPaths)
	}
}

// ---------------------------------------------------------------------------
// Per-tool drift: modify only one tool's file → check reports drift for that tool
// ---------------------------------------------------------------------------

func TestCheck_PerToolDrift(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil // same as installed
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return nil, errors.New("not found at ref")
		},
	}

	// The cursor file exists but has been modified (different content than checksum)
	fsMock := &filesystemtest.MockFilesystem{
		ReadFileFn: func(name string) ([]byte, error) {
			if strings.Contains(name, ".cursor") {
				return []byte("modified content"), nil
			}
			return nil, errors.New("not found")
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{
				Market: "mkt",
				Packages: []domain.InstalledPackage{
					{
						Profile:   "skills/bar",
						Version:   "abc123",
						Files:     domain.InstalledFiles{Skills: []string{"bar"}},
						Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}, {Path: testProjectPath(), Type: "cursor", Files: []domain.InstalledFile{{Path: ".cursor/rules/SKILL.mdc", XXH: "0000000000000000"}}}},
					},
				},
			},
		},
	}
	idb := idbWithData(db)

	transformers := domain.TransformerRegistry{
		"cursor": cursorTransformer(),
	}

	app := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers, idb)
	statuses, err := app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	// Overall state should be clean (Claude is clean)
	if statuses[0].State != domain.StateClean {
		t.Errorf("expected StateClean, got %v", statuses[0].State)
	}
	// But per-tool state should show cursor drift
	if statuses[0].ToolStates == nil {
		t.Fatal("expected ToolStates to be populated")
	}
	if statuses[0].ToolStates["cursor"] != domain.StateDrift {
		t.Errorf("expected cursor tool state to be StateDrift, got %v", statuses[0].ToolStates["cursor"])
	}
}

// ---------------------------------------------------------------------------
// Agent skip: add agent → warning for unsupported tools (Cursor, Windsurf)
// ---------------------------------------------------------------------------

func TestAdd_AgentSkipUnsupportedTools(t *testing.T) {
	writePaths := map[string]bool{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writePaths[path] = true
			return nil
		},
		StatFn: func(name string) (fs.FileInfo, error) {
			return fakeFileInfo{isDir: true}, nil
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

	transformers := domain.TransformerRegistry{
		"cursor": cursorTransformer(), // does not support agents
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers)

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude write should happen
	if !writePaths[".claude/agents/foo.md"] {
		t.Error("expected Claude file write to .claude/agents/foo.md")
	}

	// Cursor file should NOT be written (agents not supported)
	for path := range writePaths {
		if strings.Contains(path, ".cursor") {
			t.Errorf("cursor file should not be written for agents, but found: %s", path)
		}
	}
}

// ---------------------------------------------------------------------------
// Missing tool dir: enable Cursor but .cursor/ doesn't exist → warning, no crash
// ---------------------------------------------------------------------------

func TestAdd_MissingToolDir_NoCrash(t *testing.T) {
	writePaths := map[string]bool{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writePaths[path] = true
			return nil
		},
		StatFn: func(name string) (fs.FileInfo, error) {
			// .cursor directory does not exist
			if strings.Contains(name, ".cursor") {
				return nil, errors.New("not found")
			}
			return fakeFileInfo{isDir: true}, nil
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

	transformers := domain.TransformerRegistry{
		"cursor": cursorTransformer(),
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers)

	// Should NOT crash
	_, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude write should happen
	if !writePaths[".claude/skills/bar/SKILL.md"] {
		t.Error("expected Claude file write to .claude/skills/bar/SKILL.md")
	}

	// Cursor file should NOT be written (dir doesn't exist)
	for path := range writePaths {
		if strings.Contains(path, ".cursor") {
			t.Errorf("cursor file should not be written when .cursor/ missing, but found: %s", path)
		}
	}
}
