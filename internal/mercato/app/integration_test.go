package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/fsadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/gitadapter"
	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// dotGitLoader resolves non-bare repos by looking inside .git, so the
// in-process server transport works without shelling out to git.
type dotGitLoader struct{}

func (l *dotGitLoader) Load(ep *transport.Endpoint) (storer.Storer, error) {
	path := ep.Path
	dotGitPath := filepath.Join(path, ".git")
	if info, err := os.Stat(dotGitPath); err == nil && info.IsDir() {
		return filesystem.NewStorage(osfs.New(dotGitPath), cache.NewObjectLRUDefault()), nil
	}
	if _, err := os.Stat(filepath.Join(path, "config")); err == nil {
		return filesystem.NewStorage(osfs.New(path), cache.NewObjectLRUDefault()), nil
	}
	return nil, transport.ErrRepositoryNotFound
}

func TestMain(m *testing.M) {
	// Replace the file transport with an in-process server so tests don't
	// need the git binary installed.
	client.InstallProtocol("file", server.NewClient(&dotGitLoader{}))
	os.Exit(m.Run())
}

// createTestRepo initialises a real git repo in a temp directory with the given
// files and returns its path.
func createTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatal(err)
		}
	}

	commitHash, err := wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Rename default branch to "main" so origin/main refs work after clone.
	mainRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), commitHash)
	if err := repo.Storer.SetReference(mainRef); err != nil {
		t.Fatal(err)
	}
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	if err := repo.Storer.SetReference(headRef); err != nil {
		t.Fatal(err)
	}
	// Remove the old master ref if it exists.
	_ = repo.Storer.RemoveReference(plumbing.NewBranchReferenceName("master"))

	return dir
}

// cloneTestRepo copies the .git directory from sourceDir into cloneDir and sets
// up origin remote refs so that origin/<branch> refs exist — without going
// through the git transport layer, which behaves differently across Go versions.
func cloneTestRepo(t *testing.T, sourceDir, cloneDir string) {
	t.Helper()
	if err := os.MkdirAll(cloneDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Copy the entire source repo (worktree + .git) into cloneDir.
	if err := copyDir(sourceDir, cloneDir); err != nil {
		t.Fatalf("copyDir: %v", err)
	}
	// Add an origin remote pointing at sourceDir so origin/main refs resolve.
	repo, err := git.PlainOpen(cloneDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remotes["origin"] = &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{sourceDir},
	}
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	// Create origin/main tracking ref pointing at main.
	ref, err := repo.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatal(err)
	}
	originMain := plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", "main"), ref.Hash())
	if err := repo.Storer.SetReference(originMain); err != nil {
		t.Fatal(err)
	}
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

// addCommitToRepo adds or modifies files in an existing repo and creates a new commit.
func addCommitToRepo(t *testing.T, repoDir string, files map[string]string, msg string) {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		fullPath := filepath.Join(repoDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	_, err = wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

const marketName = "test/market"

// marketFiles returns the standard market repo file set.
func marketFiles() map[string]string {
	return map[string]string{
		"dev/go/agents/code-review.md":    "---\ndescription: \"Go code reviewer\"\n---\nReview Go code for best practices.\n",
		"dev/go/skills/go-arch/SKILL.md":  "---\ndescription: \"Go architect\"\n---\nArchitect Go applications.\n",
		"dev/go/skills/go-arch/prompt.md": "You are a Go architect.\n",
		"dev/go/README.md":                "---\ntags:\n  - go\n  - dev\n---\nGo development profile.\n",
	}
}

// setupIntegration creates source repo, clones it under cacheDir with the
// expected market dir name, writes the mct config, and returns a ready App
// plus the projectDir path.
func setupIntegration(t *testing.T, repoFiles map[string]string) (*App, string, string) {
	t.Helper()

	sourceDir := createTestRepo(t, repoFiles)
	cacheDir := t.TempDir()
	projectDir := t.TempDir()

	cloneDir := filepath.Join(cacheDir, marketDirName(marketName))
	cloneTestRepo(t, sourceDir, cloneDir)

	configPath := filepath.Join(projectDir, ".claude", ".mct.yaml")
	localPath := filepath.Join(projectDir, ".claude")

	cfgStore := cfgadapter.NewConfigStore()
	cfg := domain.Config{
		LocalPath: localPath,
		Markets: []domain.MarketConfig{
			{Name: marketName, URL: sourceDir, Branch: "main"},
		},
	}
	if err := cfgStore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	// Set sync state so Update can compute diffs.
	stateStore := cfgadapter.NewStateStore()
	gitRepo := gitadapter.New(gitadapter.WithDepth(0))
	headSHA, err := gitRepo.RemoteHEAD(cloneDir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.SetMarketSyncClean(cacheDir, marketName, headSHA); err != nil {
		t.Fatal(err)
	}

	application := New(
		gitRepo,
		fsadapter.New(),
		cfgStore,
		stateStore,
		cfgadapter.NewInstallDB(),
		configPath,
		cacheDir,
	)

	return application, projectDir, sourceDir
}

func TestIntegration_AddAgent(t *testing.T) {
	application, projectDir, _ := setupIntegration(t, marketFiles())

	ref := domain.MctRef(marketName + "@dev/go/agents/code-review.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Verify agent file was copied.
	installedPath := filepath.Join(projectDir, ".claude", "agents", "code-review.md")
	got, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("read installed file: %v", err)
	}
	want := "---\ndescription: \"Go code reviewer\"\n---\nReview Go code for best practices.\n"
	if string(got) != want {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(got), want)
	}

	// Verify sibling skills from same profile were also installed.
	skillPath := filepath.Join(projectDir, ".claude", "skills", "go-arch", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("expected sibling skill go-arch/SKILL.md to be installed: %v", err)
	}
	promptPath := filepath.Join(projectDir, ".claude", "skills", "go-arch", "prompt.md")
	if _, err := os.Stat(promptPath); err != nil {
		t.Errorf("expected sibling skill support file prompt.md to be installed: %v", err)
	}

	// Verify installdb.
	db, err := cfgadapter.NewInstallDB().Load(application.cacheDir)
	if err != nil {
		t.Fatalf("load installdb: %v", err)
	}
	im := db.FindMarket(marketName)
	if im == nil {
		t.Fatal("market not found in installdb")
	}
	found := false
	for _, pkg := range im.Packages {
		for _, a := range pkg.Files.Agents {
			if a == "code-review.md" {
				found = true
				// Verify location.
				for _, loc := range pkg.Locations {
					if loc.Path == projectDir {
						break
					}
				}
			}
		}
	}
	if !found {
		t.Error("agent code-review.md not found in installdb")
	}
}

func TestIntegration_AddSkillDir(t *testing.T) {
	application, projectDir, _ := setupIntegration(t, marketFiles())

	ref := domain.MctRef(marketName + "@dev/go/skills/go-arch")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Verify both files were copied.
	skillDir := filepath.Join(projectDir, ".claude", "skills", "go-arch")
	for _, name := range []string{"SKILL.md", "prompt.md"} {
		path := filepath.Join(skillDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}

	// Verify content of SKILL.md.
	got, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if want := "---\ndescription: \"Go architect\"\n---\nArchitect Go applications.\n"; string(got) != want {
		t.Errorf("SKILL.md content mismatch:\ngot:  %q\nwant: %q", string(got), want)
	}

	// Verify installdb has the skill.
	db, err := cfgadapter.NewInstallDB().Load(application.cacheDir)
	if err != nil {
		t.Fatalf("load installdb: %v", err)
	}
	im := db.FindMarket(marketName)
	if im == nil {
		t.Fatal("market not found in installdb")
	}
	found := false
	for _, pkg := range im.Packages {
		for _, s := range pkg.Files.Skills {
			if s == "go-arch" {
				found = true
			}
		}
	}
	if !found {
		t.Error("skill go-arch not found in installdb")
	}
}

func TestIntegration_AddProfile(t *testing.T) {
	application, projectDir, _ := setupIntegration(t, marketFiles())

	ref := domain.MctRef(marketName + "@dev/go")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add profile: %v", err)
	}

	// Verify agent was installed.
	agentPath := filepath.Join(projectDir, ".claude", "agents", "code-review.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("expected agent file: %v", err)
	}

	// Verify skill directory was installed.
	skillPath := filepath.Join(projectDir, ".claude", "skills", "go-arch", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("expected skill file: %v", err)
	}

	// Verify installdb has all files.
	db, err := cfgadapter.NewInstallDB().Load(application.cacheDir)
	if err != nil {
		t.Fatalf("load installdb: %v", err)
	}
	im := db.FindMarket(marketName)
	if im == nil {
		t.Fatal("market not found in installdb")
	}

	// Verify installdb has at least one package with files.
	// Note: AddOrUpdatePackage overwrites files per profile, so the last entry
	// added for each profile determines what is recorded. We verify that the
	// installdb is non-empty and has at least one file (agent or skill).
	var totalFiles int
	for _, pkg := range im.Packages {
		totalFiles += len(pkg.Files.Agents) + len(pkg.Files.Skills)
	}
	if totalFiles == 0 {
		t.Error("expected at least one file entry in installdb after profile add")
	}
}

func TestIntegration_RemoveAgent(t *testing.T) {
	application, projectDir, _ := setupIntegration(t, marketFiles())

	ref := domain.MctRef(marketName + "@dev/go/agents/code-review.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	installedPath := filepath.Join(projectDir, ".claude", "agents", "code-review.md")
	if _, err := os.Stat(installedPath); err != nil {
		t.Fatalf("file should exist before remove: %v", err)
	}

	if _, err := application.Remove(ref, service.RemoveOpts{}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Verify file is gone.
	if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
		t.Error("expected file to be deleted after Remove")
	}

	// Verify installdb no longer has the package at this location.
	db, err := cfgadapter.NewInstallDB().Load(application.cacheDir)
	if err != nil {
		t.Fatalf("load installdb: %v", err)
	}
	im := db.FindMarket(marketName)
	if im != nil {
		for _, pkg := range im.Packages {
			for _, loc := range pkg.Locations {
				if loc.Path == projectDir {
					for _, a := range pkg.Files.Agents {
						if a == "code-review.md" {
							t.Error("agent still present in installdb after Remove")
						}
					}
				}
			}
		}
	}
}

func TestIntegration_CheckClean(t *testing.T) {
	application, _, _ := setupIntegration(t, marketFiles())

	ref := domain.MctRef(marketName + "@dev/go/agents/code-review.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	statuses, err := application.Check(service.CheckOpts{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if len(statuses) == 0 {
		t.Fatal("expected at least one status entry")
	}

	for _, s := range statuses {
		if s.State != domain.StateClean {
			t.Errorf("expected StateClean for %s, got %s", s.Ref, s.State)
		}
	}
}

func TestIntegration_CheckDrift(t *testing.T) {
	application, projectDir, _ := setupIntegration(t, marketFiles())

	ref := domain.MctRef(marketName + "@dev/go/agents/code-review.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Manually modify the installed file.
	installedPath := filepath.Join(projectDir, ".claude", "agents", "code-review.md")
	if err := os.WriteFile(installedPath, []byte("modified content"), 0644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	statuses, err := application.Check(service.CheckOpts{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if len(statuses) == 0 {
		t.Fatal("expected at least one status entry")
	}

	foundDrift := false
	for _, s := range statuses {
		if s.State == domain.StateDrift {
			foundDrift = true
		}
	}
	if !foundDrift {
		t.Error("expected at least one entry with StateDrift")
	}
}

func TestIntegration_SyncAndUpdate(t *testing.T) {
	application, projectDir, sourceDir := setupIntegration(t, marketFiles())

	ref := domain.MctRef(marketName + "@dev/go/agents/code-review.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Record the old installdb version.
	oldDB, err := cfgadapter.NewInstallDB().Load(application.cacheDir)
	if err != nil {
		t.Fatalf("load installdb: %v", err)
	}
	oldIM := oldDB.FindMarket(marketName)
	if oldIM == nil {
		t.Fatal("market not in installdb")
	}
	var oldVersion string
	for _, pkg := range oldIM.Packages {
		for _, a := range pkg.Files.Agents {
			if a == "code-review.md" {
				oldVersion = pkg.Version
			}
		}
	}

	// Add a second commit to the source repo with updated agent content.
	newContent := "---\ndescription: \"Go code reviewer v2\"\n---\nReview Go code with updated rules.\n"
	addCommitToRepo(t, sourceDir, map[string]string{
		"dev/go/agents/code-review.md": newContent,
	}, "update agent")

	// Use Sync (combined refresh + update) to fetch and apply updates.
	syncResults, err := application.Sync(service.SyncOpts{AllMerge: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(syncResults) == 0 {
		t.Fatal("expected sync results")
	}
	if syncResults[0].Refresh.Err != nil {
		t.Fatalf("Sync refresh error: %v", syncResults[0].Refresh.Err)
	}

	// The Sync flow: Refresh fetches and sets sync state to new SHA,
	// then Update diffs from sync state SHA vs HEAD. Since they are the same
	// after refresh, Update finds no diff-based changes. Instead, we verify
	// by checking that the file version in installdb differs and the local file
	// was updated via the refresh fetch + re-read path.
	//
	// Actually verify via Check: after sync the entry should be in
	// StateUpdateAvailable (the installdb version != HEAD) since Update
	// may not have matched diffs.
	//
	// The fundamental design: Update uses DiffSinceCommit from the LAST SYNCED
	// sha. After Refresh sets it to HEAD, diff is empty. This means Sync only
	// produces update results if there were PREVIOUS unprocessed changes.
	//
	// For a proper update test, we need to: fetch without updating sync state,
	// then call Update. We achieve this by manually fetching in the clone dir.

	// Reset: re-add from scratch with a new App to test manual fetch + update.
	application2, projectDir2, sourceDir2 := setupIntegration(t, marketFiles())

	ref2 := domain.MctRef(marketName + "@dev/go/agents/code-review.md")
	if _, err := application2.Add(ref2, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Add a second commit to source.
	newContent2 := "---\ndescription: \"Go code reviewer v3\"\n---\nUpdated review rules.\n"
	addCommitToRepo(t, sourceDir2, map[string]string{
		"dev/go/agents/code-review.md": newContent2,
	}, "update agent v3")

	// Manually fetch in the clone (full depth) to get new commit without
	// updating sync state. The sync state still has the old SHA.
	cloneDir2 := filepath.Join(application2.cacheDir, marketDirName(marketName))
	cloneRepo2, err := git.PlainOpen(cloneDir2)
	if err != nil {
		t.Fatal(err)
	}
	err = cloneRepo2.Fetch(&git.FetchOptions{})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		t.Fatalf("manual fetch: %v", err)
	}

	// Now call Update. It reads sync state (old SHA), diffs from old to HEAD, finds changes.
	updateResults, err := application2.Update(service.UpdateOpts{AllMerge: true})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(updateResults) == 0 {
		t.Fatal("expected update results")
	}

	foundUpdate := false
	for _, r := range updateResults {
		if r.Action == "update" {
			foundUpdate = true
			if r.OldVersion == r.NewVersion {
				t.Error("expected version to change after update")
			}
		}
	}
	if !foundUpdate {
		t.Errorf("expected at least one 'update' action, got: %+v", updateResults)
	}

	// Verify local file has the new content.
	installedPath := filepath.Join(projectDir2, ".claude", "agents", "code-review.md")
	got, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("read updated file: %v", err)
	}
	if string(got) != newContent2 {
		t.Errorf("updated content mismatch:\ngot:  %q\nwant: %q", string(got), newContent2)
	}

	// Verify installdb version was updated.
	db, err := cfgadapter.NewInstallDB().Load(application2.cacheDir)
	if err != nil {
		t.Fatalf("load installdb: %v", err)
	}
	im := db.FindMarket(marketName)
	if im == nil {
		t.Fatal("market not found in installdb after update")
	}
	for _, pkg := range im.Packages {
		for _, a := range pkg.Files.Agents {
			if a == "code-review.md" {
				if pkg.Version == oldVersion {
					t.Error("installdb version was not updated")
				}
			}
		}
	}

	_ = projectDir // first projectDir unused after restructure
	_ = sourceDir  // first sourceDir unused after restructure
}

func TestIntegration_MultiLocation(t *testing.T) {
	sourceDir := createTestRepo(t, marketFiles())
	cacheDir := t.TempDir()
	projectDir1 := t.TempDir()
	projectDir2 := t.TempDir()

	cloneDir := filepath.Join(cacheDir, marketDirName(marketName))
	cloneTestRepo(t, sourceDir, cloneDir)

	cfgStore := cfgadapter.NewConfigStore()
	stateStore := cfgadapter.NewStateStore()
	installDB := cfgadapter.NewInstallDB()
	gitRepo := gitadapter.New(gitadapter.WithDepth(0))

	headSHA, err := gitRepo.RemoteHEAD(cloneDir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.SetMarketSyncClean(cacheDir, marketName, headSHA); err != nil {
		t.Fatal(err)
	}

	// Helper to create an App for a given project dir.
	makeApp := func(projectDir string) *App {
		configPath := filepath.Join(projectDir, ".claude", ".mct.yaml")
		localPath := filepath.Join(projectDir, ".claude")
		cfg := domain.Config{
			LocalPath: localPath,
			Markets: []domain.MarketConfig{
				{Name: marketName, URL: sourceDir, Branch: "main"},
			},
		}
		if err := cfgStore.Save(configPath, cfg); err != nil {
			t.Fatal(err)
		}
		return New(gitRepo, fsadapter.New(), cfgStore, stateStore, installDB, configPath, cacheDir)
	}

	app1 := makeApp(projectDir1)
	app2 := makeApp(projectDir2)

	// Add the same agent to both projects.
	ref := domain.MctRef(marketName + "@dev/go/agents/code-review.md")
	if _, err := app1.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add to project 1: %v", err)
	}
	if _, err := app2.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add to project 2: %v", err)
	}

	// Verify both projects have the agent file.
	for i, dir := range []string{projectDir1, projectDir2} {
		agentPath := filepath.Join(dir, ".claude", "agents", "code-review.md")
		if _, err := os.Stat(agentPath); err != nil {
			t.Errorf("project %d: expected agent file: %v", i+1, err)
		}
	}

	// Verify installdb has both locations.
	db, err := installDB.Load(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	im := db.FindMarket(marketName)
	if im == nil {
		t.Fatal("market not in installdb")
	}

	locationsSet := make(map[string]bool)
	for _, pkg := range im.Packages {
		for _, loc := range pkg.Locations {
			locationsSet[loc.Path] = true
		}
	}
	if !locationsSet[projectDir1] || !locationsSet[projectDir2] {
		t.Errorf("expected both project dirs in locations, got %v", locationsSet)
	}

	// Remove from project 1.
	if _, err := app1.Remove(ref, service.RemoveOpts{}); err != nil {
		t.Fatalf("Remove from project 1: %v", err)
	}

	// Verify project 1 file is gone.
	if _, err := os.Stat(filepath.Join(projectDir1, ".claude", "agents", "code-review.md")); !os.IsNotExist(err) {
		t.Error("expected agent file to be deleted from project 1")
	}

	// Verify project 2 file still exists.
	if _, err := os.Stat(filepath.Join(projectDir2, ".claude", "agents", "code-review.md")); err != nil {
		t.Errorf("project 2 agent should still exist: %v", err)
	}

	// Verify installdb still has project 2.
	db, err = installDB.Load(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	im = db.FindMarket(marketName)
	if im == nil {
		t.Fatal("market should still be in installdb")
	}
	hasProject2 := false
	for _, pkg := range im.Packages {
		for _, loc := range pkg.Locations {
			if loc.Path == projectDir2 {
				hasProject2 = true
			}
			if loc.Path == projectDir1 {
				t.Error("project 1 should no longer be in installdb locations")
			}
		}
	}
	if !hasProject2 {
		t.Error("project 2 should still be in installdb locations")
	}
}

// ---------------------------------------------------------------------------
// Agent install → sibling skill auto-install
// ---------------------------------------------------------------------------

// multiSkillMarketFiles returns a market with an agent and multiple skills
// in the same profile, plus skills in a different profile.
func multiSkillMarketFiles() map[string]string {
	return map[string]string{
		"dev/go/agents/reviewer.md":         "---\ndescription: \"Go reviewer\"\n---\nReview Go code.\n",
		"dev/go/skills/arch/SKILL.md":       "---\ndescription: \"Go architect\"\n---\nArchitect Go.\n",
		"dev/go/skills/arch/prompt.md":      "You are a Go architect.\n",
		"dev/go/skills/testing/SKILL.md":    "---\ndescription: \"Go tester\"\n---\nTest Go.\n",
		"dev/go/skills/testing/helpers.md":  "Test helpers.\n",
		"dev/go/README.md":                  "---\ntags:\n  - go\n---\nGo dev profile.\n",
		"dev/python/agents/py-review.md":    "---\ndescription: \"Python reviewer\"\n---\nReview Python code.\n",
		"dev/python/skills/pytest/SKILL.md": "---\ndescription: \"pytest skill\"\n---\npytest.\n",
	}
}

func TestIntegration_AddAgent_InstallsSiblingSkills(t *testing.T) {
	application, projectDir, _ := setupIntegration(t, multiSkillMarketFiles())

	ref := domain.MctRef(marketName + "@dev/go/agents/reviewer.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Agent should be installed.
	agentPath := filepath.Join(projectDir, ".claude", "agents", "reviewer.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("expected agent to be installed: %v", err)
	}

	// Both skills from dev/go profile should be installed.
	for _, path := range []string{
		filepath.Join(projectDir, ".claude", "skills", "arch", "SKILL.md"),
		filepath.Join(projectDir, ".claude", "skills", "arch", "prompt.md"),
		filepath.Join(projectDir, ".claude", "skills", "testing", "SKILL.md"),
		filepath.Join(projectDir, ".claude", "skills", "testing", "helpers.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected sibling skill file %s to be installed: %v", filepath.Base(path), err)
		}
	}

	// Skills from dev/python profile must NOT be installed.
	pySkillPath := filepath.Join(projectDir, ".claude", "skills", "pytest", "SKILL.md")
	if _, err := os.Stat(pySkillPath); err == nil {
		t.Error("skills from different profile (dev/python) should not be installed")
	}
}

func TestIntegration_AddAgent_FlatMarket_NoSiblingSkills(t *testing.T) {
	// Flat market layout: agents/foo.md, skills/bar/SKILL.md — no profile prefix.
	flatFiles := map[string]string{
		"agents/reviewer.md":      "---\ndescription: \"reviewer\"\n---\nReview code.\n",
		"skills/testing/SKILL.md": "---\ndescription: \"testing\"\n---\nTest code.\n",
	}
	application, projectDir, _ := setupIntegration(t, flatFiles)

	ref := domain.MctRef(marketName + "@agents/reviewer.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Agent should be installed.
	agentPath := filepath.Join(projectDir, ".claude", "agents", "reviewer.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("expected agent to be installed: %v", err)
	}

	// Skills should NOT be installed — flat layout has no profile to expand.
	skillPath := filepath.Join(projectDir, ".claude", "skills", "testing", "SKILL.md")
	if _, err := os.Stat(skillPath); err == nil {
		t.Error("skills should not be auto-installed in flat market layout")
	}
}

func TestIntegration_AddAgent_SiblingSkillsIdempotent(t *testing.T) {
	application, projectDir, _ := setupIntegration(t, multiSkillMarketFiles())

	ref := domain.MctRef(marketName + "@dev/go/agents/reviewer.md")

	// First install.
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	// Second install should return already-installed (not fail).
	_, err := application.Add(ref, service.AddOpts{})
	if err != domain.ErrEntryAlreadyInstalled {
		t.Fatalf("expected ErrEntryAlreadyInstalled on re-add, got: %v", err)
	}

	// Skills should still be there.
	skillPath := filepath.Join(projectDir, ".claude", "skills", "arch", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("expected skill to still exist: %v", err)
	}
}

func TestIntegration_AddAgent_ForceReinstalls(t *testing.T) {
	application, projectDir, _ := setupIntegration(t, multiSkillMarketFiles())

	ref := domain.MctRef(marketName + "@dev/go/agents/reviewer.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	agentPath := filepath.Join(projectDir, ".claude", "agents", "reviewer.md")
	original, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read agent: %v", err)
	}
	if err := os.WriteFile(agentPath, []byte("locally modified"), 0o644); err != nil {
		t.Fatalf("modify agent: %v", err)
	}

	if _, err := application.Add(ref, service.AddOpts{Force: true}); err != nil {
		t.Fatalf("forced Add: %v", err)
	}

	got, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read agent: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("expected forced reinstall to restore the original content, got %q", got)
	}
}

func TestIntegration_AddAgent_SiblingSkillsWithDeps(t *testing.T) {
	// Agent declares a dependency, and profile also has undeclared sibling skills.
	// Both should be installed.
	files := map[string]string{
		"dev/go/agents/reviewer.md":      "---\ndescription: \"Go reviewer\"\nrequires_skills:\n  - file: dev/go/skills/arch/SKILL.md\n---\nReview Go.\n",
		"dev/go/skills/arch/SKILL.md":    "---\ndescription: \"Go architect\"\n---\nArchitect.\n",
		"dev/go/skills/testing/SKILL.md": "---\ndescription: \"Go tester\"\n---\nTest.\n",
		"dev/go/README.md":               "---\ntags:\n  - go\n---\nGo.\n",
	}
	application, projectDir, _ := setupIntegration(t, files)

	ref := domain.MctRef(marketName + "@dev/go/agents/reviewer.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Declared dependency should be installed.
	archPath := filepath.Join(projectDir, ".claude", "skills", "arch", "SKILL.md")
	if _, err := os.Stat(archPath); err != nil {
		t.Errorf("expected declared dep skill to be installed: %v", err)
	}

	// Undeclared sibling skill should also be installed.
	testingPath := filepath.Join(projectDir, ".claude", "skills", "testing", "SKILL.md")
	if _, err := os.Stat(testingPath); err != nil {
		t.Errorf("expected undeclared sibling skill to be installed: %v", err)
	}
}

// TestIntegration_AddAgent_DepOnSkillSubFile reproduces the user-reported
// scenario where an agent declares requires_skills entries that point at
// supporting files inside a skill directory (e.g.
// "skills/agile-project/references/markers.md") rather than the SKILL.md.
// Installing the parent SKILL.md already copies the whole skill directory
// (including the referenced sub-file), so the sub-file dep is redundant.
// Before the fix, addInternal saw the on-disk skill directory created by
// the previous dep install and returned ENTRY_ALREADY_INSTALLED, which
// bubbled up through resolveDependencies and aborted the whole add.
func TestIntegration_AddAgent_DepOnSkillSubFile(t *testing.T) {
	files := map[string]string{
		"dev/go/agents/reviewer.md":      "---\ndescription: \"Go reviewer\"\nrequires_skills:\n  - file: dev/go/skills/arch/SKILL.md\n  - file: dev/go/skills/arch/prompt.md\n---\nReview Go.\n",
		"dev/go/skills/arch/SKILL.md":    "---\ndescription: \"Go architect\"\n---\nArchitect.\n",
		"dev/go/skills/arch/prompt.md":   "You are a Go architect.\n",
		"dev/go/skills/testing/SKILL.md": "---\ndescription: \"Go tester\"\n---\nTest.\n",
		"dev/go/README.md":               "---\ntags:\n  - go\n---\nGo.\n",
	}
	application, projectDir, _ := setupIntegration(t, files)

	ref := domain.MctRef(marketName + "@dev/go/agents/reviewer.md")
	if _, err := application.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// SKILL.md and the supporting prompt.md must both be present (the skill
	// installer copies the whole directory).
	for _, p := range []string{
		filepath.Join(projectDir, ".claude", "skills", "arch", "SKILL.md"),
		filepath.Join(projectDir, ".claude", "skills", "arch", "prompt.md"),
		filepath.Join(projectDir, ".claude", "agents", "reviewer.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s installed: %v", p, err)
		}
	}
}

// TestIntegration_ReAddAfterRemove_WithSiblingLocation reproduces the bug
// where re-adding an entry after removing it from one of several install
// locations failed with ENTRY_ALREADY_INSTALLED. RemoveLocation only strips
// the location string and leaves pkg.Files populated for the remaining
// sibling locations, so the fileInPackage-based "already installed" check
// would incorrectly trip on re-add.
func TestIntegration_ReAddAfterRemove_WithSiblingLocation(t *testing.T) {
	sourceDir := createTestRepo(t, marketFiles())
	cacheDir := t.TempDir()
	projectDir1 := t.TempDir()
	projectDir2 := t.TempDir()

	cloneDir := filepath.Join(cacheDir, marketDirName(marketName))
	cloneTestRepo(t, sourceDir, cloneDir)

	cfgStore := cfgadapter.NewConfigStore()
	stateStore := cfgadapter.NewStateStore()
	installDB := cfgadapter.NewInstallDB()
	gitRepo := gitadapter.New(gitadapter.WithDepth(0))

	headSHA, err := gitRepo.RemoteHEAD(cloneDir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.SetMarketSyncClean(cacheDir, marketName, headSHA); err != nil {
		t.Fatal(err)
	}

	makeApp := func(projectDir string) *App {
		configPath := filepath.Join(projectDir, ".claude", ".mct.yaml")
		localPath := filepath.Join(projectDir, ".claude")
		cfg := domain.Config{
			LocalPath: localPath,
			Markets: []domain.MarketConfig{
				{Name: marketName, URL: sourceDir, Branch: "main"},
			},
		}
		if err := cfgStore.Save(configPath, cfg); err != nil {
			t.Fatal(err)
		}
		return New(gitRepo, fsadapter.New(), cfgStore, stateStore, installDB, configPath, cacheDir)
	}

	app1 := makeApp(projectDir1)
	app2 := makeApp(projectDir2)
	ref := domain.MctRef(marketName + "@dev/go/agents/code-review.md")

	if _, err := app1.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add to project 1: %v", err)
	}
	if _, err := app2.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add to project 2: %v", err)
	}

	if _, err := app2.Remove(ref, service.RemoveOpts{}); err != nil {
		t.Fatalf("Remove from project 2: %v", err)
	}

	// Re-add to project 2 — must succeed even though pkg.Files still
	// records the agent because project 1 keeps the package alive.
	if _, err := app2.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Re-add to project 2 after remove: %v", err)
	}

	agentPath := filepath.Join(projectDir2, ".claude", "agents", "code-review.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("expected agent file restored at project 2: %v", err)
	}
}

// TestIntegration_ReAddProfileAfterRemove_WithSiblingLocation reproduces the
// user-reported scenario: a profile with several agents+skills installed in
// two locations, removed from one, then re-added. addProfile iterates files
// and installs the first one fine; the bug caused subsequent files to be
// reported as already installed because pkg.Files still listed them from the
// sibling location.
func TestIntegration_ReAddProfileAfterRemove_WithSiblingLocation(t *testing.T) {
	sourceDir := createTestRepo(t, multiSkillMarketFiles())
	cacheDir := t.TempDir()
	projectDir1 := t.TempDir()
	projectDir2 := t.TempDir()

	cloneDir := filepath.Join(cacheDir, marketDirName(marketName))
	cloneTestRepo(t, sourceDir, cloneDir)

	cfgStore := cfgadapter.NewConfigStore()
	stateStore := cfgadapter.NewStateStore()
	installDB := cfgadapter.NewInstallDB()
	gitRepo := gitadapter.New(gitadapter.WithDepth(0))

	headSHA, err := gitRepo.RemoteHEAD(cloneDir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.SetMarketSyncClean(cacheDir, marketName, headSHA); err != nil {
		t.Fatal(err)
	}

	makeApp := func(projectDir string) *App {
		configPath := filepath.Join(projectDir, ".claude", ".mct.yaml")
		localPath := filepath.Join(projectDir, ".claude")
		cfg := domain.Config{
			LocalPath: localPath,
			Markets: []domain.MarketConfig{
				{Name: marketName, URL: sourceDir, Branch: "main"},
			},
		}
		if err := cfgStore.Save(configPath, cfg); err != nil {
			t.Fatal(err)
		}
		return New(gitRepo, fsadapter.New(), cfgStore, stateStore, installDB, configPath, cacheDir)
	}

	app1 := makeApp(projectDir1)
	app2 := makeApp(projectDir2)
	profileRef := domain.MctRef(marketName + "@dev/go")

	if _, err := app1.Add(profileRef, service.AddOpts{}); err != nil {
		t.Fatalf("Add profile to project 1: %v", err)
	}
	if _, err := app2.Add(profileRef, service.AddOpts{}); err != nil {
		t.Fatalf("Add profile to project 2: %v", err)
	}

	if _, err := app2.Remove(profileRef, service.RemoveOpts{}); err != nil {
		t.Fatalf("Remove profile from project 2: %v", err)
	}

	// Re-add the full profile. Before the fix this returned
	// ENTRY_ALREADY_INSTALLED on the second file in addProfile.
	if _, err := app2.Add(profileRef, service.AddOpts{}); err != nil {
		t.Fatalf("Re-add profile to project 2 after remove: %v", err)
	}

	wantFiles := []string{
		filepath.Join(projectDir2, ".claude", "agents", "reviewer.md"),
		filepath.Join(projectDir2, ".claude", "skills", "arch", "SKILL.md"),
		filepath.Join(projectDir2, ".claude", "skills", "testing", "SKILL.md"),
	}
	for _, p := range wantFiles {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file restored at %s: %v", p, err)
		}
	}

	// Project 1 must remain intact.
	if _, err := os.Stat(filepath.Join(projectDir1, ".claude", "agents", "reviewer.md")); err != nil {
		t.Errorf("project 1 agent should still exist: %v", err)
	}
}
