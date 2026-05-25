package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore/configstoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/filesystem/filesystemtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo/gitrepotest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/installdb/installdbtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore/statestoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
)

// ---------------------------------------------------------------------------
// Helper: opencode transformer that supports agents
// ---------------------------------------------------------------------------

func opencodeTransformer() *fakeTransformer {
	return &fakeTransformer{
		name:           "opencode",
		supportsAgents: true,
		supportsSkills: true,
		extension:      ".md",
		dotDir:         ".opencode",
	}
}

// ---------------------------------------------------------------------------
// 1. Install skill to multiple tools (claude + cursor + opencode)
// ---------------------------------------------------------------------------

func TestMultiTool_InstallSkillToMultipleTools(t *testing.T) {
	writePaths := map[string][]byte{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true, "opencode": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writePaths[path] = content
			return nil
		},
		StatFn: func(name string) (fs.FileInfo, error) {
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
		"opencode": opencodeTransformer(),
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers)

	result, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude write
	if _, ok := writePaths[".claude/skills/bar/SKILL.md"]; !ok {
		t.Error("expected Claude file write to .claude/skills/bar/SKILL.md")
	}

	// Check cursor and opencode
	projectDir := testProjectPath()
	foundCursor := false
	foundOpencode := false
	for path := range writePaths {
		if strings.HasPrefix(path, projectDir+"/.cursor/") {
			foundCursor = true
		}
		if strings.HasPrefix(path, projectDir+"/.opencode/") {
			foundOpencode = true
		}
	}
	if !foundCursor {
		t.Errorf("expected cursor file write, got paths: %v", keys(writePaths))
	}
	if !foundOpencode {
		t.Errorf("expected opencode file write, got paths: %v", keys(writePaths))
	}

	// Verify AddResult has tool writes
	if len(result.ToolWrites) < 2 {
		t.Errorf("expected at least 2 tool writes in result, got %d: %v", len(result.ToolWrites), result.ToolWrites)
	}
}

// ---------------------------------------------------------------------------
// 2. Agent skip for unsupported tools
// ---------------------------------------------------------------------------

func TestMultiTool_AgentSkipUnsupportedTools(t *testing.T) {
	writePaths := map[string]bool{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true, "opencode": true},
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
		"cursor":   cursorTransformer(),   // does NOT support agents
		"opencode": opencodeTransformer(), // DOES support agents
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers)

	result, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude agent should be written
	if !writePaths[".claude/agents/foo.md"] {
		t.Error("expected Claude file write to .claude/agents/foo.md")
	}

	// OpenCode agent should be written
	projectDir := testProjectPath()
	foundOpencode := false
	for path := range writePaths {
		if strings.Contains(path, ".opencode") {
			foundOpencode = true
		}
	}
	if !foundOpencode {
		t.Errorf("expected opencode agent file write, got: %v", writePaths)
	}

	// Cursor should NOT be written (does not support agents)
	for path := range writePaths {
		if strings.Contains(path, ".cursor") {
			t.Errorf("cursor file should not be written for agents, but found: %s", path)
		}
	}

	// AddResult should have warning about cursor skip
	foundCursorWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "cursor") && strings.Contains(w, "skip") {
			foundCursorWarning = true
			break
		}
	}
	if !foundCursorWarning {
		t.Errorf("expected warning about cursor agent skip, got warnings: %v", result.Warnings)
	}

	_ = projectDir
}

// ---------------------------------------------------------------------------
// 3. Remove from multiple tools
// ---------------------------------------------------------------------------

func TestMultiTool_RemoveFromMultipleTools(t *testing.T) {
	deletedPaths := map[string]bool{}
	removedPaths := map[string]bool{}

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
								Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
							},
						},
					},
				},
			}, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	transformers := domain.TransformerRegistry{
		"cursor":   cursorTransformer(),
		"windsurf": windsurfTransformer(),
	}

	a := newMultiToolApp(cfg, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{}, transformers, idb)

	_, err := a.Remove("mkt@skills/bar/SKILL.md", service.RemoveOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude removal
	if !removedPaths[".claude/skills/bar"] {
		t.Errorf("expected .claude/skills/bar to be removed, got: %v", removedPaths)
	}

	// Cursor removal
	foundCursorDelete := false
	for path := range deletedPaths {
		if strings.Contains(path, ".cursor") {
			foundCursorDelete = true
		}
	}
	if !foundCursorDelete {
		t.Errorf("expected cursor file deletion, got deleted paths: %v", deletedPaths)
	}

	// Windsurf removal
	foundWindsurfDelete := false
	for path := range deletedPaths {
		if strings.Contains(path, ".windsurf") {
			foundWindsurfDelete = true
		}
	}
	if !foundWindsurfDelete {
		t.Errorf("expected windsurf file deletion, got deleted paths: %v", deletedPaths)
	}
}

// ---------------------------------------------------------------------------
// 4. Per-tool drift detection
// ---------------------------------------------------------------------------

func TestMultiTool_PerToolDriftDetection(t *testing.T) {
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
			return "abc123", nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return nil, errors.New("not found at ref")
		},
	}

	// Claude file: gone (simulates no drift detection for Claude since ReadFileAtRef errors)
	// Cursor file: exists but modified (different content than recorded checksum)
	fsMock := &filesystemtest.MockFilesystem{
		ReadFileFn: func(name string) ([]byte, error) {
			if strings.Contains(name, ".cursor") {
				return []byte("modified cursor content"), nil
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

	// Claude state should be clean (same version)
	if statuses[0].State != domain.StateClean {
		t.Errorf("expected StateClean for Claude, got %v", statuses[0].State)
	}

	// Per-tool state should show cursor drift
	if statuses[0].ToolStates == nil {
		t.Fatal("expected ToolStates to be populated")
	}
	if statuses[0].ToolStates["cursor"] != domain.StateDrift {
		t.Errorf("expected cursor state StateDrift, got %v", statuses[0].ToolStates["cursor"])
	}
}

// ---------------------------------------------------------------------------
// 5. Config migration: no tools field defaults to claude-only
// ---------------------------------------------------------------------------

func TestMultiTool_ConfigMigrationDefaultsClaude(t *testing.T) {
	// Config with nil Tools field (old config before multi-tool feature)
	writePaths := map[string]bool{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     nil, // no tools section
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
			return skillFile(), nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/SKILL.md"}, nil
		},
	}

	// Register cursor transformer but Tools is nil -> cursor should NOT be invoked
	transformers := domain.TransformerRegistry{
		"cursor": cursorTransformer(),
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers)

	_, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude write should happen (normal code path, not transformer-based)
	if !writePaths[".claude/skills/bar/SKILL.md"] {
		t.Error("expected Claude file write")
	}

	// Cursor should NOT be written (Tools is nil => no enabled tools)
	for path := range writePaths {
		if strings.Contains(path, ".cursor") {
			t.Errorf("cursor should not be written when Tools is nil, but found: %s", path)
		}
	}
}

// ---------------------------------------------------------------------------
// 6. Per-project override: global {cursor: false} + .mct.yml {cursor: true}
// ---------------------------------------------------------------------------

func TestMultiTool_PerProjectOverride(t *testing.T) {
	writePaths := map[string]bool{}

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": false},
			}, nil
		},
		LoadProjectConfigFn: func(projectDir string) (domain.ProjectConfig, error) {
			return domain.ProjectConfig{
				Tools: map[string]bool{"cursor": true},
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

	_, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cursor should be written because project override enables it
	projectDir := testProjectPath()
	foundCursor := false
	for path := range writePaths {
		if strings.HasPrefix(path, projectDir+"/.cursor/") {
			foundCursor = true
		}
	}
	if !foundCursor {
		t.Errorf("expected cursor file write due to project override, got paths: %v", writePaths)
	}
}

// ---------------------------------------------------------------------------
// 7. Missing tool directory: cursor enabled but .cursor/ doesn't exist
// ---------------------------------------------------------------------------

func TestMultiTool_MissingToolDirectory(t *testing.T) {
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

	result, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Claude should still be written
	if !writePaths[".claude/skills/bar/SKILL.md"] {
		t.Error("expected Claude file write")
	}

	// Cursor should NOT be written (directory missing)
	for path := range writePaths {
		if strings.Contains(path, ".cursor") {
			t.Errorf("cursor file should not be written when .cursor/ missing: %s", path)
		}
	}

	// Should have a warning about cursor
	foundWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "cursor") && strings.Contains(w, "does not exist") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("expected warning about missing .cursor directory, got: %v", result.Warnings)
	}
}

// ---------------------------------------------------------------------------
// 8. Gemini plain markdown: no frontmatter in output
// ---------------------------------------------------------------------------

func TestMultiTool_GeminiPlainMarkdown(t *testing.T) {
	var geminiContent []byte

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"gemini": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			if strings.Contains(path, ".gemini") {
				geminiContent = content
			}
			return nil
		},
		StatFn: func(name string) (fs.FileInfo, error) {
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

	// Use REAL GeminiTransformer to verify no frontmatter
	transformers := domain.TransformerRegistry{
		"gemini": &transform.GeminiTransformer{},
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers)

	_, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if geminiContent == nil {
		t.Fatal("expected gemini file to be written")
	}

	// Gemini output should NOT have frontmatter delimiters
	content := string(geminiContent)
	if strings.Contains(content, "---") {
		t.Errorf("gemini output should not contain frontmatter delimiters, got:\n%s", content)
	}

	// Should contain the body
	if !strings.Contains(content, "Skill") || !strings.Contains(content, "Do stuff.") {
		t.Errorf("gemini output missing expected body content, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// 9. OpenCode model mapping: agent with model=opus -> mapped model
// ---------------------------------------------------------------------------

func TestMultiTool_OpenCodeModelMapping(t *testing.T) {
	var opencodeContent []byte

	agentWithModel := []byte("---\ndescription: test agent\nmodel: opus\n---\n# Agent\nDo smart things.\n")

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"opencode": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			if strings.Contains(path, ".opencode") {
				opencodeContent = content
			}
			return nil
		},
		StatFn: func(name string) (fs.FileInfo, error) {
			return fakeFileInfo{isDir: true}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return agentWithModel, nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
	}

	// Use REAL OpenCodeTransformer with custom tool mapping store
	mappingStore := &fakeToolMappingStoreWithMappings{
		mapping: domain.ToolMapping{
			Models: map[string]map[string]string{
				"opus": {
					"opencode": "anthropic/claude-opus-4-20250514",
				},
			},
			Tools: make(map[string]map[string]string),
		},
	}

	transformers := domain.TransformerRegistry{
		"opencode": &transform.OpenCodeTransformer{},
	}

	a := New(git, fsMock, cfg, &statestoretest.MockStateStore{},
		&installdbtest.MockInstallDB{
			LockFn:   func(cacheDir string) error { return nil },
			UnlockFn: func(cacheDir string) error { return nil },
			LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
				return domain.InstallDatabase{Markets: []domain.InstalledMarket{}}, nil
			},
			SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
		},
		"/config/path", "/cache/dir",
		WithTransformers(transformers),
		WithToolMappings(mappingStore),
	)

	_, err := a.Add("mkt@agents/foo.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opencodeContent == nil {
		t.Fatal("expected opencode file to be written")
	}

	content := string(opencodeContent)
	if !strings.Contains(content, "anthropic/claude-opus-4-20250514") {
		t.Errorf("expected mapped model in opencode output, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fakeToolMappingStoreWithMappings returns configured mappings.
type fakeToolMappingStoreWithMappings struct {
	mapping domain.ToolMapping
}

func (f *fakeToolMappingStoreWithMappings) LoadToolMappings(_ string) (domain.ToolMapping, error) {
	return f.mapping, nil
}
func (f *fakeToolMappingStoreWithMappings) SaveToolMappings(_ string, _ domain.ToolMapping) error {
	return nil
}
func (f *fakeToolMappingStoreWithMappings) ToolMappingsExist(_ string) bool { return true }
func (f *fakeToolMappingStoreWithMappings) DefaultToolMappings() domain.ToolMapping {
	return f.mapping
}

func keys[V any](m map[string]V) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func TestMultiTool_InstallRecordsAllToolLocations(t *testing.T) {
	var savedDBData []byte

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true, "opencode": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			if strings.Contains(path, "installed.json") {
				savedDBData = content
			}
			return nil
		},
		StatFn: func(name string) (fs.FileInfo, error) {
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
		"opencode": opencodeTransformer(),
	}
	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{Markets: []domain.InstalledMarket{}}, nil
		},
		MarshalFn: func(db domain.InstallDatabase) ([]byte, error) {
			return json.Marshal(db)
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers, idb)

	_, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedDBData == nil {
		t.Fatal("install DB was never written — missing installed.json write")
	}

	var savedDB domain.InstallDatabase
	if err := json.Unmarshal(savedDBData, &savedDB); err != nil {
		t.Fatalf("failed to parse saved install DB: %v", err)
	}

	im := savedDB.FindMarket("mkt")
	if im == nil {
		t.Fatal("market 'mkt' not found in saved install DB")
	}
	pkg := savedDB.FindPackage("mkt", "skills/bar")
	if pkg == nil {
		t.Fatal("package not found in saved install DB")
	}

	projectPath := testProjectPath()

	var foundClaude, foundCursor, foundOpencode bool
	for _, loc := range pkg.Locations {
		if loc.Path != projectPath {
			t.Errorf("unexpected location path %q, want %q", loc.Path, projectPath)
		}
		switch loc.Type {
		case domain.RuntimeTypeClaudeCode:
			foundClaude = true
			if len(loc.Files) == 0 {
				t.Error("ClaudeCode location should have files")
			}
		case "cursor":
			foundCursor = true
			if len(loc.Files) == 0 {
				t.Error("cursor location should have files")
			}
		case "opencode":
			foundOpencode = true
			if len(loc.Files) == 0 {
				t.Error("opencode location should have files")
			}
		default:
			t.Errorf("unexpected location type %q", loc.Type)
		}
	}
	if !foundClaude {
		t.Error("install DB missing ClaudeCode location")
	}
	if !foundCursor {
		t.Error("install DB missing cursor location")
	}
	if !foundOpencode {
		t.Error("install DB missing opencode location")
	}
	if len(pkg.Locations) != 3 {
		t.Errorf("expected 3 locations (claude + cursor + opencode), got %d", len(pkg.Locations))
	}
}

func TestMultiTool_AddOptsToolsOverride_RecordsOnlySpecifiedTools(t *testing.T) {
	var savedDBData []byte

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true, "opencode": true},
			}, nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			if strings.Contains(path, "installed.json") {
				savedDBData = content
			}
			return nil
		},
		StatFn: func(name string) (fs.FileInfo, error) {
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
		"opencode": opencodeTransformer(),
	}
	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return domain.InstallDatabase{Markets: []domain.InstalledMarket{}}, nil
		},
		MarshalFn: func(db domain.InstallDatabase) ([]byte, error) {
			return json.Marshal(db)
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	a := newMultiToolApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, transformers, idb)

	result, err := a.Add("mkt@skills/bar/SKILL.md", service.AddOpts{Tools: map[string]bool{"cursor": true}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedDBData == nil {
		t.Fatal("install DB was never written")
	}

	var savedDB domain.InstallDatabase
	if err := json.Unmarshal(savedDBData, &savedDB); err != nil {
		t.Fatalf("failed to parse saved install DB: %v", err)
	}

	pkg := savedDB.FindPackage("mkt", "skills/bar")
	if pkg == nil {
		t.Fatal("package not found in saved install DB")
	}

	var foundClaude, foundCursor, foundOpencode bool
	for _, loc := range pkg.Locations {
		switch loc.Type {
		case domain.RuntimeTypeClaudeCode:
			foundClaude = true
		case "cursor":
			foundCursor = true
		case "opencode":
			foundOpencode = true
		}
	}
	if !foundClaude {
		t.Error("install DB should always have ClaudeCode location")
	}
	if !foundCursor {
		t.Error("install DB should have cursor location (explicitly overridden)")
	}
	if foundOpencode {
		t.Error("install DB should NOT have opencode location (config enables it but Tools override limits to cursor only)")
	}
	if len(pkg.Locations) != 2 {
		t.Errorf("expected 2 locations (claude + cursor), got %d", len(pkg.Locations))
	}

	_ = result
}

func TestMultiTool_UpdatePreservesToolLocations(t *testing.T) {
	var savedDBData []byte

	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
				Tools:     map[string]bool{"cursor": true},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{
				Version: 1,
				Markets: map[string]domain.MarketSyncState{
					"mkt": {LastSyncedSHA: "oldhash"},
				},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return []domain.FileDiff{
				{Action: domain.DiffModify, From: "skills/bar/SKILL.md", To: "skills/bar/SKILL.md"},
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return skillFile(), nil
			}
			return nil, errors.New("not found at ref")
		},
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/SKILL.md"}, nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "newhash", nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			if strings.Contains(path, "installed.json") {
				savedDBData = content
			}
			return nil
		},
		DeleteFileFn: func(path string) error { return nil },
		StatFn: func(name string) (fs.FileInfo, error) {
			return fakeFileInfo{isDir: true}, nil
		},
	}

	projectPath := testProjectPath()
	existingDB := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{
				Market: "mkt",
				Packages: []domain.InstalledPackage{
					{
						Profile: "skills/bar",
						Version: "oldhash",
						Files:   domain.InstalledFiles{Skills: []string{"bar"}},
						Locations: []domain.InstalledLocation{
							{Path: projectPath, Type: domain.RuntimeTypeClaudeCode},
							{
								Path:  projectPath,
								Type:  "cursor",
								Files: []domain.InstalledFile{{Path: ".cursor/skills/bar.md", XXH: "aaa"}},
							},
						},
					},
				},
			},
		},
	}
	idb := &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return existingDB, nil
		},
		MarshalFn: func(db domain.InstallDatabase) ([]byte, error) {
			return json.Marshal(db)
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}

	transformers := domain.TransformerRegistry{
		"cursor": cursorTransformer(),
	}
	a := newMultiToolApp(cfg, git, fsMock, state, transformers, idb)

	results, err := a.Update(service.UpdateOpts{AllMerge: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 update result")
	}

	if savedDBData == nil {
		t.Fatal("install DB was never written after update")
	}

	var savedDB domain.InstallDatabase
	if err := json.Unmarshal(savedDBData, &savedDB); err != nil {
		t.Fatalf("failed to parse saved install DB: %v", err)
	}

	pkg := savedDB.FindPackage("mkt", "skills/bar")
	if pkg == nil {
		t.Fatal("package not found after update")
	}

	var foundClaude, foundCursor bool
	for _, loc := range pkg.Locations {
		switch loc.Type {
		case domain.RuntimeTypeClaudeCode:
			foundClaude = true
		case "cursor":
			foundCursor = true
			if len(loc.Files) == 0 {
				t.Error("cursor location should still have files after update")
			}
		}
	}
	if !foundClaude {
		t.Error("ClaudeCode location missing after update")
	}
	if !foundCursor {
		t.Error("cursor location missing after update — tool locations are not preserved")
	}
	if len(pkg.Locations) != 2 {
		t.Errorf("expected 2 locations (claude + cursor) after update, got %d", len(pkg.Locations))
	}
}
