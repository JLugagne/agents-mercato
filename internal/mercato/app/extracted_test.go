package app

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore/configstoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/filesystem/filesystemtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo/gitrepotest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore/statestoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// ---------------------------------------------------------------------------
// findAffectedPackages
// ---------------------------------------------------------------------------

func TestFindAffectedPackages_MatchesByFile(t *testing.T) {
	im := &domain.InstalledMarket{
		Market: "mkt",
		Packages: []domain.InstalledPackage{
			{Profile: "dev/go", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}},
			{Profile: "dev/py", Files: domain.InstalledFiles{Skills: []string{"bar"}}},
		},
	}
	diffs := []domain.FileDiff{
		{Action: domain.DiffModify, From: "dev/go/agents/foo.md", To: "dev/go/agents/foo.md"},
	}
	mc := domain.MarketConfig{Name: "mkt", Branch: "main"}

	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})
	affected := a.findAffectedPackages(im, diffs, mc)

	if len(affected) != 1 {
		t.Fatalf("expected 1 affected package, got %d", len(affected))
	}
	if affected[0].Profile != "dev/go" {
		t.Errorf("expected profile dev/go, got %q", affected[0].Profile)
	}
}

func TestFindAffectedPackages_SkipsDeleted(t *testing.T) {
	im := &domain.InstalledMarket{
		Market: "mkt",
		Packages: []domain.InstalledPackage{
			{Profile: "dev/go", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}},
		},
	}
	diffs := []domain.FileDiff{
		{Action: domain.DiffDelete, From: "dev/go/agents/foo.md", To: ""},
	}
	mc := domain.MarketConfig{Name: "mkt", Branch: "main"}

	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})
	affected := a.findAffectedPackages(im, diffs, mc)

	if len(affected) != 0 {
		t.Errorf("expected 0 affected (deletes skipped), got %d", len(affected))
	}
}

func TestFindAffectedPackages_SkillsOnlyFiltering(t *testing.T) {
	im := &domain.InstalledMarket{
		Market: "mkt",
		Packages: []domain.InstalledPackage{
			{Profile: "agents/foo.md", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}},
			{Profile: "skills/bar", Files: domain.InstalledFiles{Skills: []string{"bar"}}},
		},
	}
	diffs := []domain.FileDiff{
		{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
		{Action: domain.DiffModify, From: "skills/bar/SKILL.md", To: "skills/bar/SKILL.md"},
	}
	mc := domain.MarketConfig{Name: "mkt", Branch: "main", SkillsOnly: true, SkillsPath: "skills"}

	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})
	affected := a.findAffectedPackages(im, diffs, mc)

	// Only the skill package should match — agent path is filtered out by SkillsOnly.
	if len(affected) != 1 {
		t.Fatalf("expected 1 affected (skills only), got %d", len(affected))
	}
	for _, pkg := range affected {
		if pkg.Profile != "skills/bar" {
			t.Errorf("expected skills/bar, got %q", pkg.Profile)
		}
	}
}

func TestFindAffectedPackages_NoMatch(t *testing.T) {
	im := &domain.InstalledMarket{
		Market: "mkt",
		Packages: []domain.InstalledPackage{
			{Profile: "dev/go", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}},
		},
	}
	diffs := []domain.FileDiff{
		{Action: domain.DiffModify, From: "dev/py/agents/bar.md", To: "dev/py/agents/bar.md"},
	}
	mc := domain.MarketConfig{Name: "mkt", Branch: "main"}

	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})
	affected := a.findAffectedPackages(im, diffs, mc)

	if len(affected) != 0 {
		t.Errorf("expected 0 affected, got %d", len(affected))
	}
}

// ---------------------------------------------------------------------------
// mergeAddResult
// ---------------------------------------------------------------------------

func TestMergeAddResult_Empty(t *testing.T) {
	result := &service.AddResult{}
	mergeAddResult(result, toolWriteResult{})

	if result.ToolWrites != nil {
		t.Error("expected nil ToolWrites when merging empty result")
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(result.Warnings))
	}
}

func TestMergeAddResult_WithToolWrites(t *testing.T) {
	result := &service.AddResult{}
	twr := toolWriteResult{
		ToolWrites: map[string]string{"cursor": ".cursor/rules/foo.mdc"},
		Warnings:   []string{"windsurf: skipped"},
	}
	mergeAddResult(result, twr)

	if result.ToolWrites["cursor"] != ".cursor/rules/foo.mdc" {
		t.Errorf("expected cursor write path, got %q", result.ToolWrites["cursor"])
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != "windsurf: skipped" {
		t.Errorf("expected warning, got %v", result.Warnings)
	}
}

func TestMergeAddResult_AccumulatesWarnings(t *testing.T) {
	result := &service.AddResult{
		Warnings: []string{"existing warning"},
	}
	twr := toolWriteResult{
		Warnings: []string{"new warning"},
	}
	mergeAddResult(result, twr)

	if len(result.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(result.Warnings))
	}
}

// ---------------------------------------------------------------------------
// installEntryFiles
// ---------------------------------------------------------------------------

func TestInstallEntryFiles_Agent(t *testing.T) {
	var writtenPath string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writtenPath = path
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	files, _, err := a.installEntryFiles(directWriter{fs: fsMock}, "/clone", "main", "agents/foo.md", "/project/agents/foo.md", []byte("content"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Agents) != 1 || files.Agents[0] != "foo.md" {
		t.Errorf("expected agent foo.md, got %v", files.Agents)
	}
	if len(files.Skills) != 0 {
		t.Errorf("expected no skills, got %v", files.Skills)
	}
	if writtenPath != "/project/agents/foo.md" {
		t.Errorf("expected write to /project/agents/foo.md, got %q", writtenPath)
	}
}

func TestInstallEntryFiles_FlatSkill(t *testing.T) {
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	files, _, err := a.installEntryFiles(directWriter{fs: fsMock}, "/clone", "main", "skills/bar.md", "/project/skills/bar.md", []byte("content"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Skills) != 1 || files.Skills[0] != "bar" {
		t.Errorf("expected skill bar, got %v", files.Skills)
	}
}

func TestInstallEntryFiles_DirBasedSkill(t *testing.T) {
	var writtenPaths []string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writtenPaths = append(writtenPaths, path)
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{"skills/baz/SKILL.md", "skills/baz/helper.md"}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return []byte("file content for " + filePath), nil
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	files, _, err := a.installEntryFiles(directWriter{fs: fsMock}, "/clone", "main", "skills/baz/SKILL.md", "/project/skills/baz", []byte("unused"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Skills) != 1 || files.Skills[0] != "baz" {
		t.Errorf("expected skill baz, got %v", files.Skills)
	}
	if len(writtenPaths) != 2 {
		t.Fatalf("expected 2 files written, got %d", len(writtenPaths))
	}
	if writtenPaths[0] != "/project/skills/baz/SKILL.md" {
		t.Errorf("expected /project/skills/baz/SKILL.md, got %q", writtenPaths[0])
	}
	if writtenPaths[1] != "/project/skills/baz/helper.md" {
		t.Errorf("expected /project/skills/baz/helper.md, got %q", writtenPaths[1])
	}
}

func TestInstallEntryFiles_DirBasedSkill_PreservesSubdirs(t *testing.T) {
	var writtenPaths []string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writtenPaths = append(writtenPaths, path)
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{
				"skills/baz/SKILL.md",
				"skills/baz/references/structure.md",
				"skills/baz/scripts/status.sh",
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return []byte("file content for " + filePath), nil
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	files, _, err := a.installEntryFiles(directWriter{fs: fsMock}, "/clone", "main", "skills/baz/SKILL.md", "/project/skills/baz", []byte("unused"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Skills) != 1 || files.Skills[0] != "baz" {
		t.Errorf("expected skill baz, got %v", files.Skills)
	}
	if len(writtenPaths) != 3 {
		t.Fatalf("expected 3 files written, got %d", len(writtenPaths))
	}
	if writtenPaths[0] != "/project/skills/baz/SKILL.md" {
		t.Errorf("expected /project/skills/baz/SKILL.md, got %q", writtenPaths[0])
	}
	if writtenPaths[1] != "/project/skills/baz/references/structure.md" {
		t.Errorf("expected /project/skills/baz/references/structure.md, got %q", writtenPaths[1])
	}
	if writtenPaths[2] != "/project/skills/baz/scripts/status.sh" {
		t.Errorf("expected /project/skills/baz/scripts/status.sh, got %q", writtenPaths[2])
	}
}

func TestInstallEntryFiles_WriteError(t *testing.T) {
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			return errors.New("disk full")
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, &gitrepotest.MockGitRepo{}, fsMock, &statestoretest.MockStateStore{})

	_, _, err := a.installEntryFiles(directWriter{fs: fsMock}, "/clone", "main", "agents/foo.md", "/project/agents/foo.md", []byte("content"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("expected disk full error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// walkMarketEntries / validateProfiles / validateDeps
// ---------------------------------------------------------------------------

func TestWalkMarketEntries_CollectsProfileData(t *testing.T) {
	fsys := fstest.MapFS{
		"dev/go/README.md":           {Data: []byte("---\ntags:\n  - golang\n---\n# README\n")},
		"dev/go/agents/foo.md":       {Data: []byte("---\ndescription: test\n---\n# foo\n")},
		"dev/go/skills/bar/SKILL.md": {Data: []byte("---\ndescription: skill\n---\n# bar\n")},
		"dev/py/agents/baz.md":       {Data: []byte("---\ndescription: py agent\n---\n# baz\n")},
	}

	var result service.LintResult
	w, err := walkMarketEntries(fsys, ".", &result)
	if err != nil {
		t.Fatal(err)
	}

	if len(w.profileOrder) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(w.profileOrder))
	}

	goProfile := w.profiles["dev/go"]
	if goProfile == nil {
		t.Fatal("expected dev/go profile")
	}
	if !goProfile.hasReadme {
		t.Error("expected dev/go to have README")
	}
	if !goProfile.hasTags {
		t.Error("expected dev/go README to have tags")
	}
	if len(goProfile.agents) != 1 {
		t.Errorf("expected 1 agent in dev/go, got %d", len(goProfile.agents))
	}
	if len(goProfile.skills) != 1 {
		t.Errorf("expected 1 skill in dev/go, got %d", len(goProfile.skills))
	}

	if _, ok := w.knownPaths["dev/go/agents/foo.md"]; !ok {
		t.Error("expected dev/go/agents/foo.md in known paths")
	}
	if _, ok := w.knownPaths["dev/go/skills/bar/SKILL.md"]; !ok {
		t.Error("expected dev/go/skills/bar/SKILL.md in known paths")
	}
}

func TestWalkMarketEntries_BadFrontmatter(t *testing.T) {
	fsys := fstest.MapFS{
		"dev/go/agents/bad.md": {Data: []byte("no frontmatter here")},
	}

	var result service.LintResult
	_, err := walkMarketEntries(fsys, ".", &result)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}
	if result.Issues[0].Severity != "error" {
		t.Errorf("expected error severity, got %q", result.Issues[0].Severity)
	}
}

func TestValidateProfiles_MissingReadme(t *testing.T) {
	w := lintWalkResult{
		profiles: map[string]*lintProfileData{
			"dev/go": {agents: []string{"dev/go/agents/foo.md"}},
		},
		profileOrder: []string{"dev/go"},
	}

	var result service.LintResult
	validateProfiles(w, &result)

	if result.Profiles != 1 {
		t.Errorf("expected 1 profile, got %d", result.Profiles)
	}
	if result.Agents != 1 {
		t.Errorf("expected 1 agent, got %d", result.Agents)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Severity == "warn" && strings.Contains(issue.Message, "missing README") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing README warning")
	}
}

func TestValidateProfiles_ReadmeNoTags(t *testing.T) {
	w := lintWalkResult{
		profiles: map[string]*lintProfileData{
			"dev/go": {agents: []string{"a"}, hasReadme: true, hasTags: false},
		},
		profileOrder: []string{"dev/go"},
	}

	var result service.LintResult
	validateProfiles(w, &result)

	found := false
	for _, issue := range result.Issues {
		if issue.Severity == "warn" && strings.Contains(issue.Message, "no tags") {
			found = true
		}
	}
	if !found {
		t.Error("expected no-tags warning")
	}
}

func TestValidateProfiles_EmptyProfile(t *testing.T) {
	w := lintWalkResult{
		profiles: map[string]*lintProfileData{
			"dev/go": {hasReadme: true, hasTags: true},
		},
		profileOrder: []string{"dev/go"},
	}

	var result service.LintResult
	validateProfiles(w, &result)

	found := false
	for _, issue := range result.Issues {
		if issue.Severity == "warn" && strings.Contains(issue.Message, "no agents or skills") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty profile warning")
	}
}

func TestValidateDeps_MissingDep(t *testing.T) {
	w := lintWalkResult{
		profiles:   map[string]*lintProfileData{"dev/go": {}},
		knownPaths: map[string]struct{}{},
		agentDeps: []lintAgentDeps{
			{
				agentRel: "dev/go/agents/foo.md",
				deps:     []domain.SkillDep{{File: "dev/go/skills/missing.md"}},
			},
		},
	}

	var result service.LintResult
	validateDeps(w, &result)

	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}
	if !strings.Contains(result.Issues[0].Message, "missing file") {
		t.Errorf("expected missing file message, got %q", result.Issues[0].Message)
	}
}

func TestValidateDeps_FoundDep(t *testing.T) {
	w := lintWalkResult{
		profiles:   map[string]*lintProfileData{"dev/go": {}},
		knownPaths: map[string]struct{}{"dev/go/skills/bar.md": {}},
		agentDeps: []lintAgentDeps{
			{
				agentRel: "dev/go/agents/foo.md",
				deps:     []domain.SkillDep{{File: "dev/go/skills/bar.md"}},
			},
		},
	}

	var result service.LintResult
	validateDeps(w, &result)

	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %v", result.Issues)
	}
}

// ---------------------------------------------------------------------------
// copyUpdatedFiles
// ---------------------------------------------------------------------------

func TestCopyUpdatedFiles_Skills(t *testing.T) {
	var writtenPaths []string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writtenPaths = append(writtenPaths, path)
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ListDirFilesFn: func(clonePath, branch, dirPrefix string) ([]string, error) {
			return []string{dirPrefix + "/SKILL.md"}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return []byte("skill content"), nil
		},
	}

	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	ctx := updateCtx{
		mc:        domain.MarketConfig{Name: "mkt", Branch: "main"},
		pkg:       &domain.InstalledPackage{Profile: "dev/go", Files: domain.InstalledFiles{Skills: []string{"bar"}}},
		clonePath: "/cache/mkt",
		cfg:       domain.Config{LocalPath: ".claude"},
	}

	files, _ := a.copyUpdatedFiles(directWriter{fs: fsMock}, ctx)
	if len(files.Skills) != 1 || files.Skills[0] != "bar" {
		t.Errorf("expected skill bar, got %v", files.Skills)
	}
	if len(writtenPaths) != 1 {
		t.Errorf("expected 1 write, got %d", len(writtenPaths))
	}
}

func TestCopyUpdatedFiles_Agents(t *testing.T) {
	var writtenPath string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writtenPath = path
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return []byte("agent content"), nil
		},
	}

	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	ctx := updateCtx{
		mc:        domain.MarketConfig{Name: "mkt", Branch: "main"},
		pkg:       &domain.InstalledPackage{Profile: "dev/go", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}},
		clonePath: "/cache/mkt",
		cfg:       domain.Config{LocalPath: ".claude"},
	}

	files, _ := a.copyUpdatedFiles(directWriter{fs: fsMock}, ctx)
	if len(files.Agents) != 1 || files.Agents[0] != "foo.md" {
		t.Errorf("expected agent foo.md, got %v", files.Agents)
	}
	if writtenPath != ".claude/agents/foo.md" {
		t.Errorf("expected .claude/agents/foo.md, got %q", writtenPath)
	}
}

// ---------------------------------------------------------------------------
// updatePackageAtLocation — drift paths
// ---------------------------------------------------------------------------

func TestUpdatePackageAtLocation_DriftKeep(t *testing.T) {
	git := &gitrepotest.MockGitRepo{
		// Return content so drift detection finds drift (os.ReadFile on
		// non-existent local files will fail => drift)
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return []byte("original"), nil
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	im := &domain.InstalledMarket{
		Market: "mkt",
		Packages: []domain.InstalledPackage{
			{Profile: "dev/go", Version: "old", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}, Locations: []domain.InstalledLocation{{Path: "/proj", Type: domain.RuntimeTypeClaudeCode}}},
		},
	}

	result := a.updatePackageAtLocation(updateCtx{
		mc:        domain.MarketConfig{Name: "mkt", Branch: "main"},
		im:        im,
		pkg:       &im.Packages[0],
		pkgIdx:    0,
		location:  "/proj",
		clonePath: "/cache/mkt",
		cfg:       domain.Config{LocalPath: ".claude"},
		opts:      service.UpdateOpts{AllKeep: true},
	})

	if result.Action != "kept" {
		t.Errorf("expected action=kept, got %q", result.Action)
	}
	if len(result.DriftFiles) == 0 {
		t.Error("expected drift files to be populated")
	}
}

func TestUpdatePackageAtLocation_DriftReport(t *testing.T) {
	git := &gitrepotest.MockGitRepo{
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return []byte("original"), nil
		},
	}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})

	im := &domain.InstalledMarket{
		Market: "mkt",
		Packages: []domain.InstalledPackage{
			{Profile: "dev/go", Version: "old", Files: domain.InstalledFiles{Agents: []string{"foo.md"}}, Locations: []domain.InstalledLocation{{Path: "/proj", Type: domain.RuntimeTypeClaudeCode}}},
		},
	}

	result := a.updatePackageAtLocation(updateCtx{
		mc:        domain.MarketConfig{Name: "mkt", Branch: "main"},
		im:        im,
		pkg:       &im.Packages[0],
		pkgIdx:    0,
		location:  "/proj",
		clonePath: "/cache/mkt",
		cfg:       domain.Config{LocalPath: ".claude"},
		opts:      service.UpdateOpts{}, // no AllKeep, no AllMerge → report drift
	})

	if result.Action != "drift" {
		t.Errorf("expected action=drift, got %q", result.Action)
	}
}

func TestInstallEntryFiles_Command(t *testing.T) {
	var writtenPath string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writtenPath = path
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{}
	a := newTestApp(&configstoretest.MockConfigStore{}, git, fsMock, &statestoretest.MockStateStore{})

	files, written, err := a.installEntryFiles(directWriter{fs: fsMock}, "/clone", "main", "commands/foo.md", "/project/.claude/commands/foo.md", []byte("content"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Commands) != 1 || files.Commands[0] != "foo.md" {
		t.Errorf("expected command foo.md, got %v", files.Commands)
	}
	if len(files.Agents) != 0 || len(files.Skills) != 0 {
		t.Errorf("expected no agents or skills, got agents=%v skills=%v", files.Agents, files.Skills)
	}
	if writtenPath != "/project/.claude/commands/foo.md" {
		t.Errorf("unexpected write path %q", writtenPath)
	}
	if len(written) != 1 {
		t.Fatalf("expected 1 InstalledFile, got %d", len(written))
	}
	if !strings.HasSuffix(written[0].Path, ".claude/commands/foo.md") {
		t.Errorf("unexpected installed path %q", written[0].Path)
	}
	if written[0].XXH == "" {
		t.Errorf("expected non-empty XXH hash")
	}
}
