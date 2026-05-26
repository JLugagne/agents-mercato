package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore/configstoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/filesystem/filesystemtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo/gitrepotest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/installdb/installdbtest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore/statestoretest"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// newTestApp constructs an App for testing.
func newTestApp(cfg *configstoretest.MockConfigStore, git *gitrepotest.MockGitRepo, fs *filesystemtest.MockFilesystem, state *statestoretest.MockStateStore, idbOpts ...*installdbtest.MockInstallDB) *App {
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
	return New(git, fs, cfg, state, idb, "/config/path", "/cache/dir")
}

// testProjectPath returns the absolute project path that projectPath(".claude")
// resolves to in the test process. Matches what the app code computes.
func testProjectPath() string {
	abs, _ := filepath.Abs(".claude")
	return filepath.Dir(abs)
}

// installedDB returns an installdb with one package at the test project location.
func installedDB(market, profile, version string, files domain.InstalledFiles) domain.InstallDatabase {
	return domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{
				Market: market,
				Packages: []domain.InstalledPackage{
					{
						Profile:   profile,
						Version:   version,
						Files:     files,
						Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}},
					},
				},
			},
		},
	}
}

// idbWithData returns a MockInstallDB that returns the given database.
func idbWithData(db domain.InstallDatabase) *installdbtest.MockInstallDB {
	return &installdbtest.MockInstallDB{
		LockFn:   func(cacheDir string) error { return nil },
		UnlockFn: func(cacheDir string) error { return nil },
		LoadFn: func(cacheDir string) (domain.InstallDatabase, error) {
			return db, nil
		},
		SaveFn: func(cacheDir string, db domain.InstallDatabase) error { return nil },
	}
}

// ---------------------------------------------------------------------------
// Check
// ---------------------------------------------------------------------------

// TestCheck_Clean verifies that matching versions produce StateClean.
func TestCheck_Clean(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil // same as installed version
		},
		// ReadFileAtRef returns error to skip drift detection (file not found at ref)
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return nil, errors.New("not found at ref")
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	db := installedDB("mkt", "agents/foo.md", "abc123", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)
	statuses, err := app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != domain.StateClean {
		t.Errorf("expected StateClean, got %v", statuses[0].State)
	}
}

// TestCheck_Drift verifies that a locally-modified file (present on disk
// but with a different content hash than recorded) produces StateDrift via
// the v2 per-location hash path.
func TestCheck_Drift(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil // same version => no update available
		},
	}
	// File exists on disk but its hash will not match the recorded XXH.
	fsMock := &filesystemtest.MockFilesystem{
		ReadFileFn: func(name string) ([]byte, error) {
			return []byte("locally modified"), nil
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/foo.md",
					Version: "abc123",
					Files:   domain.InstalledFiles{Agents: []string{"foo.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: testProjectPath(),
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/foo.md", XXH: "originalhash"},
							},
						},
					},
				},
			}},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)
	statuses, err := app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != domain.StateDrift {
		t.Errorf("expected StateDrift, got %v", statuses[0].State)
	}
}

// TestCheck_LocallyDeleted verifies that a file recorded as installed but
// missing from disk produces StateLocallyDeleted (not StateDrift).
func TestCheck_LocallyDeleted(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil // same version => no update available
		},
	}
	// All ReadFile calls report ENOENT — file vanished from disk.
	fsMock := &filesystemtest.MockFilesystem{
		ReadFileFn: func(name string) ([]byte, error) {
			return nil, &fs.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/foo.md",
					Version: "abc123",
					Files:   domain.InstalledFiles{Agents: []string{"foo.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: testProjectPath(),
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/foo.md", XXH: "originalhash"},
							},
						},
					},
				},
			}},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)
	statuses, err := app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != domain.StateLocallyDeleted {
		t.Errorf("expected StateLocallyDeleted, got %v", statuses[0].State)
	}
}

// TestCheck_DriftWinsOverDeleted verifies that when a package has both
// modified and deleted files, the state is StateDrift (modified takes
// precedence — it carries more risk than a missing file).
func TestCheck_DriftWinsOverDeleted(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil // same version
		},
	}
	// File "foo.md" exists with bad hash; file "bar.md" is gone.
	fsMock := &filesystemtest.MockFilesystem{
		ReadFileFn: func(name string) ([]byte, error) {
			if filepath.Base(name) == "foo.md" {
				return []byte("modified"), nil
			}
			return nil, &fs.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/group",
					Version: "abc123",
					Files:   domain.InstalledFiles{Agents: []string{"foo.md", "bar.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: testProjectPath(),
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/foo.md", XXH: "originalhash"},
								{Path: ".claude/agents/bar.md", XXH: "originalhash"},
							},
						},
					},
				},
			}},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)
	statuses, err := app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	for _, s := range statuses {
		if s.State != domain.StateDrift {
			t.Errorf("expected StateDrift for %s, got %v", s.Ref, s.State)
		}
	}
}

// TestDetectDriftSplit_Categorizes verifies that detectDriftSplit splits
// modified vs deleted correctly when called directly. Exercises the v2
// per-location hash path which is shared across Check, Sync and (later) Doctor.
func TestDetectDriftSplit_Categorizes(t *testing.T) {
	fsMock := &filesystemtest.MockFilesystem{
		ReadFileFn: func(name string) ([]byte, error) {
			switch filepath.Base(name) {
			case "kept.md":
				return []byte("k"), nil // hash will match
			case "modified.md":
				return []byte("changed"), nil // hash will not match
			default:
				return nil, &fs.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
			}
		},
	}

	pkg := domain.InstalledPackage{
		Profile: "p",
		Version: "v1",
		Files:   domain.InstalledFiles{Agents: []string{"kept.md", "modified.md", "deleted.md"}},
		Locations: []domain.InstalledLocation{
			{
				Path: "/proj",
				Type: domain.RuntimeTypeClaudeCode,
				Files: []domain.InstalledFile{
					{Path: ".claude/agents/kept.md", XXH: xxhashHex([]byte("k"))},
					{Path: ".claude/agents/modified.md", XXH: xxhashHex([]byte("original"))},
					{Path: ".claude/agents/deleted.md", XXH: xxhashHex([]byte("anything"))},
				},
			},
		},
	}

	app := newTestApp(nil, nil, fsMock, nil, nil)
	modified, deleted := app.detectDriftSplit(pkg, "/proj", "/clone", "main")

	if len(modified) != 1 || modified[0] != ".claude/agents/modified.md" {
		t.Errorf("expected modified=[modified.md], got %v", modified)
	}
	if len(deleted) != 1 || deleted[0] != ".claude/agents/deleted.md" {
		t.Errorf("expected deleted=[deleted.md], got %v", deleted)
	}
}

// TestCheck_UpdateAvailable verifies that a newer remote HEAD produces StateUpdateAvailable.
func TestCheck_UpdateAvailable(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "newsha456", nil // different from installed version
		},
		// Return error so drift detection skips all files (no drift)
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return nil, errors.New("not found at ref")
		},
	}
	fsMock := &filesystemtest.MockFilesystem{}

	db := installedDB("mkt", "agents/foo.md", "oldsha123", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)
	statuses, err := app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != domain.StateUpdateAvailable {
		t.Errorf("expected StateUpdateAvailable, got %v", statuses[0].State)
	}
}

// TestCheck_UpdateAndDrift verifies StateUpdateAndDrift when both conditions hold.
func TestCheck_UpdateAndDrift(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "newsha456", nil // different from installed version => update available
		},
	}
	// File exists on disk but its hash will not match the recorded XXH.
	fsMock := &filesystemtest.MockFilesystem{
		ReadFileFn: func(name string) ([]byte, error) {
			return []byte("locally modified"), nil
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{
					Profile: "agents/foo.md",
					Version: "oldsha123",
					Files:   domain.InstalledFiles{Agents: []string{"foo.md"}},
					Locations: []domain.InstalledLocation{
						{
							Path: testProjectPath(),
							Type: domain.RuntimeTypeClaudeCode,
							Files: []domain.InstalledFile{
								{Path: ".claude/agents/foo.md", XXH: "originalhash"},
							},
						},
					},
				},
			}},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, &statestoretest.MockStateStore{}, idb)
	statuses, err := app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != domain.StateUpdateAndDrift {
		t.Errorf("expected StateUpdateAndDrift, got %v", statuses[0].State)
	}
}

// TestCheck_MarketFilter verifies that market filtering works.
func TestCheck_MarketFilter(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets: []domain.MarketConfig{
					{Name: "alpha", Branch: "main"},
					{Name: "beta", Branch: "main"},
				},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "abc123", nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return nil, errors.New("skip")
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "alpha", Packages: []domain.InstalledPackage{
				{Profile: "agents/a.md", Version: "abc123", Files: domain.InstalledFiles{Agents: []string{"a.md"}}, Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}}},
			}},
			{Market: "beta", Packages: []domain.InstalledPackage{
				{Profile: "agents/b.md", Version: "abc123", Files: domain.InstalledFiles{Agents: []string{"b.md"}}, Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}}},
			}},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{}, idb)
	statuses, err := app.Check(service.CheckOpts{Market: "beta"})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status (only beta), got %d", len(statuses))
	}
	if statuses[0].Ref.Market() != "beta" {
		t.Errorf("expected market=beta, got %q", statuses[0].Ref.Market())
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

// TestRefresh_Success verifies that a successful fetch produces a complete result.
func TestRefresh_Success(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{}
	git := &gitrepotest.MockGitRepo{}
	fsMock := &filesystemtest.MockFilesystem{}
	state := &statestoretest.MockStateStore{}

	cfg.LoadFn = func(path string) (domain.Config, error) {
		return domain.Config{
			Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
		}, nil
	}
	state.LoadSyncStateFn = func(cacheDir string) (domain.SyncState, error) {
		return domain.SyncState{
			Version: 1,
			Markets: map[string]domain.MarketSyncState{
				"mkt": {LastSyncedSHA: "oldhash"},
			},
		}, nil
	}
	dirtyCalledWith := ""
	state.SetMarketSyncDirtyFn = func(cacheDir, market string) error {
		dirtyCalledWith = market
		return nil
	}
	cleanCalledWith := ""
	state.SetMarketSyncCleanFn = func(cacheDir, market, newSHA string) error {
		cleanCalledWith = market
		return nil
	}
	git.FetchFn = func(clonePath, branch string) (string, error) {
		return "newhash", nil
	}
	git.DiffSinceCommitFn = func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
		return []domain.FileDiff{
			{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
			{Action: domain.DiffModify, From: "agents/bar.md", To: "agents/bar.md"},
		}, nil
	}

	app := newTestApp(cfg, git, fsMock, state)
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Market != "mkt" {
		t.Errorf("expected Market=mkt, got %q", r.Market)
	}
	if r.OldSHA != "oldhash" {
		t.Errorf("expected OldSHA=oldhash, got %q", r.OldSHA)
	}
	if r.NewSHA != "newhash" {
		t.Errorf("expected NewSHA=newhash, got %q", r.NewSHA)
	}
	if r.ChangedFiles != 2 {
		t.Errorf("expected ChangedFiles=2, got %d", r.ChangedFiles)
	}
	if r.Err != nil {
		t.Errorf("expected no error, got %v", r.Err)
	}
	if dirtyCalledWith != "mkt" {
		t.Errorf("SetMarketSyncDirty not called with mkt, got %q", dirtyCalledWith)
	}
	if cleanCalledWith != "mkt" {
		t.Errorf("SetMarketSyncClean not called with mkt, got %q", cleanCalledWith)
	}
}

// TestRefresh_UpToDate verifies that when SHA doesn't change, ChangedFiles=0.
func TestRefresh_UpToDate(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{}
	git := &gitrepotest.MockGitRepo{}
	fsMock := &filesystemtest.MockFilesystem{}
	state := &statestoretest.MockStateStore{}

	cfg.LoadFn = func(path string) (domain.Config, error) {
		return domain.Config{
			Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
		}, nil
	}
	state.LoadSyncStateFn = func(cacheDir string) (domain.SyncState, error) {
		return domain.SyncState{
			Version: 1,
			Markets: map[string]domain.MarketSyncState{
				"mkt": {LastSyncedSHA: "samehash"},
			},
		}, nil
	}
	git.FetchFn = func(clonePath, branch string) (string, error) {
		return "samehash", nil
	}
	git.DiffSinceCommitFn = func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
		return nil, nil
	}
	state.SetMarketSyncDirtyFn = func(cacheDir, market string) error { return nil }
	state.SetMarketSyncCleanFn = func(cacheDir, market, newSHA string) error { return nil }

	app := newTestApp(cfg, git, fsMock, state)
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ChangedFiles != 0 {
		t.Errorf("expected ChangedFiles=0, got %d", results[0].ChangedFiles)
	}
}

// TestRefresh_DryRun verifies that DryRun skips state mutations but still fetches.
func TestRefresh_DryRun(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{}
	git := &gitrepotest.MockGitRepo{}
	fsMock := &filesystemtest.MockFilesystem{}
	state := &statestoretest.MockStateStore{}

	cfg.LoadFn = func(path string) (domain.Config, error) {
		return domain.Config{
			Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
		}, nil
	}
	state.LoadSyncStateFn = func(cacheDir string) (domain.SyncState, error) {
		return domain.SyncState{
			Version: 1,
			Markets: map[string]domain.MarketSyncState{
				"mkt": {LastSyncedSHA: "oldhash"},
			},
		}, nil
	}
	dirtyCalled := false
	state.SetMarketSyncDirtyFn = func(cacheDir, market string) error {
		dirtyCalled = true
		return nil
	}
	cleanCalled := false
	state.SetMarketSyncCleanFn = func(cacheDir, market, newSHA string) error {
		cleanCalled = true
		return nil
	}
	fetchCalled := false
	git.FetchFn = func(clonePath, branch string) (string, error) {
		fetchCalled = true
		return "newhash", nil
	}
	git.DiffSinceCommitFn = func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
		return nil, nil
	}

	app := newTestApp(cfg, git, fsMock, state)
	_, err := app.Refresh(service.RefreshOpts{DryRun: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if !fetchCalled {
		t.Error("expected Fetch to be called in dry-run mode")
	}
	if dirtyCalled {
		t.Error("SetMarketSyncDirty should NOT be called in dry-run mode")
	}
	if cleanCalled {
		t.Error("SetMarketSyncClean should NOT be called in dry-run mode")
	}
}

// TestRefresh_UpdatesAvailable verifies that UpdatesAvailable counts packages
// whose version differs from the new SHA.
func TestRefresh_UpdatesAvailable(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
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
		SetMarketSyncDirtyFn: func(cacheDir, market string) error { return nil },
		SetMarketSyncCleanFn: func(cacheDir, market, newSHA string) error { return nil },
	}
	git := &gitrepotest.MockGitRepo{
		FetchFn: func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return nil, nil
		},
		FileVersionFn: func(clonePath, filePath string) (domain.MctVersion, error) {
			return domain.MctVersion("v"), nil
		},
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{Market: "mkt", Packages: []domain.InstalledPackage{
				{Profile: "agents/a.md", Version: "oldhash", Files: domain.InstalledFiles{Agents: []string{"a.md"}}, Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}}},
				{Profile: "agents/b.md", Version: "newhash", Files: domain.InstalledFiles{Agents: []string{"b.md"}}, Locations: []domain.InstalledLocation{{Path: testProjectPath(), Type: domain.RuntimeTypeClaudeCode}}},
			}},
		},
	}
	idb := idbWithData(db)

	fsMock := &filesystemtest.MockFilesystem{
		StatFn: func(name string) (fs.FileInfo, error) {
			return fakeDirInfo{name: name}, nil
		},
	}
	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].UpdatesAvailable != 1 {
		t.Errorf("expected UpdatesAvailable=1, got %d", results[0].UpdatesAvailable)
	}
}

// TestSyncState verifies that SyncState() returns what the state store returns.
func TestSyncState(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{}
	git := &gitrepotest.MockGitRepo{}
	fsMock := &filesystemtest.MockFilesystem{}
	state := &statestoretest.MockStateStore{}

	want := domain.SyncState{
		Version: 1,
		Markets: map[string]domain.MarketSyncState{
			"mkt": {LastSyncedSHA: "abc123"},
		},
	}
	state.LoadSyncStateFn = func(cacheDir string) (domain.SyncState, error) {
		return want, nil
	}

	app := newTestApp(cfg, git, fsMock, state)
	got, err := app.SyncState()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if got.Version != want.Version {
		t.Errorf("expected Version=%d, got %d", want.Version, got.Version)
	}
	if ms, ok := got.Markets["mkt"]; !ok || ms.LastSyncedSHA != "abc123" {
		t.Errorf("unexpected markets state: %+v", got.Markets)
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// TestUpdate_Success verifies that an installed entry with upstream changes
// reports action=update.
func TestUpdate_Success(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
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
				{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
			}, nil
		},
		// ReadFileAtRef for drift detection: return error to skip drift
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return []byte("new content"), nil
			}
			return nil, errors.New("not found at ref")
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "newhash", nil
		},
	}

	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn:  func(path string, content []byte) error { return nil },
		DeleteFileFn: func(path string) error { return nil },
	}

	db := installedDB("mkt", "agents/foo.md", "oldhash", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Update(service.UpdateOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "update" {
		t.Errorf("expected Action=update, got %q", results[0].Action)
	}
	if results[0].NewVersion != "newhash" {
		t.Errorf("expected NewVersion=newhash, got %q", results[0].NewVersion)
	}
}

// TestUpdate_NothingInstalled verifies that Update returns empty results when
// there are no installed entries that match the diff.
func TestUpdate_NothingInstalled(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{}
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
	git.DiffSinceCommitFn = func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
		return []domain.FileDiff{
			{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
		}, nil
	}

	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, state)
	results, err := app.Update(service.UpdateOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results with no installed entries, got %d", len(results))
	}
}

// TestUpdate_DeletedEntrySkipped verifies that deleted diffs are skipped.
func TestUpdate_DeletedEntrySkipped(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
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
				{Action: domain.DiffDelete, From: "agents/foo.md", To: ""},
			}, nil
		},
	}

	db := installedDB("mkt", "agents/foo.md", "oldhash", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, state, idb)
	results, err := app.Update(service.UpdateOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for deleted entry, got %d", len(results))
	}
}

// TestUpdate_DriftAllKeep verifies that drift + AllKeep produces action=kept.
func TestUpdate_DriftAllKeep(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
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
				{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
			}, nil
		},
		// Return content so drift detection runs; os.ReadFile will fail => drift
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return []byte("original"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		MD5ChecksumFn: func(content []byte) string { return "hash" },
	}

	db := installedDB("mkt", "agents/foo.md", "oldhash", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Update(service.UpdateOpts{AllKeep: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "kept" {
		t.Errorf("expected Action=kept, got %q", results[0].Action)
	}
	if len(results[0].DriftFiles) == 0 {
		t.Error("expected DriftFiles to be populated")
	}
}

// TestUpdate_DriftAllMerge verifies that drift + AllMerge overwrites and reports update.
func TestUpdate_DriftAllMerge(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
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
				{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return []byte("new content"), nil
			}
			return []byte("original"), nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "newhash", nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		MD5ChecksumFn: func(content []byte) string { return "hash" },
		WriteFileFn:   func(path string, content []byte) error { return nil },
		DeleteFileFn:  func(path string) error { return nil },
	}

	db := installedDB("mkt", "agents/foo.md", "oldhash", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Update(service.UpdateOpts{AllMerge: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "update" {
		t.Errorf("expected Action=update, got %q", results[0].Action)
	}
	if results[0].NewVersion != "newhash" {
		t.Errorf("expected NewVersion=newhash, got %q", results[0].NewVersion)
	}
}

// ---------------------------------------------------------------------------
// Refresh error paths
// ---------------------------------------------------------------------------

// TestRefresh_ConfigLoadError verifies that a config load error is returned.
func TestRefresh_ConfigLoadError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{}, errors.New("config broken")
		},
	}
	app := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, &statestoretest.MockStateStore{})
	_, err := app.Refresh(service.RefreshOpts{})
	if err == nil {
		t.Fatal("expected error from config load, got nil")
	}
	if err.Error() != "config broken" {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRefresh_LoadSyncStateError verifies that a LoadSyncState error is returned.
func TestRefresh_LoadSyncStateError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}}}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{}, errors.New("state corrupted")
		},
	}
	app := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, state)
	_, err := app.Refresh(service.RefreshOpts{})
	if err == nil {
		t.Fatal("expected error from LoadSyncState, got nil")
	}
	if err.Error() != "state corrupted" {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRefresh_FetchError verifies that a Fetch error is recorded in the result.
func TestRefresh_FetchError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}}}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{
				"mkt": {LastSyncedSHA: "oldhash"},
			}}, nil
		},
		SetMarketSyncDirtyFn: func(cacheDir string, market string) error {
			return nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		FetchFn: func(clonePath, branch string) (string, error) {
			return "", errors.New("network down")
		},
	}
	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, state)
	results, err := app.Refresh(service.RefreshOpts{})
	if err != nil {
		t.Fatal("unexpected top-level error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected error in result, got nil")
	}
	if results[0].Err.Error() != "network down" {
		t.Errorf("unexpected error: %v", results[0].Err)
	}
	if results[0].OldSHA != "oldhash" {
		t.Errorf("expected OldSHA=oldhash, got %q", results[0].OldSHA)
	}
}

// TestRefresh_SetMarketSyncDirtyError verifies that a SetMarketSyncDirty error
// is recorded in the result when DryRun=false.
func TestRefresh_SetMarketSyncDirtyError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}}}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{}}, nil
		},
		SetMarketSyncDirtyFn: func(cacheDir, market string) error {
			return errors.New("dirty failed")
		},
	}
	app := newTestApp(cfg, &gitrepotest.MockGitRepo{}, &filesystemtest.MockFilesystem{}, state)
	results, err := app.Refresh(service.RefreshOpts{DryRun: false})
	if err != nil {
		t.Fatal("unexpected top-level error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil || results[0].Err.Error() != "dirty failed" {
		t.Errorf("expected 'dirty failed' error, got %v", results[0].Err)
	}
}

// TestRefresh_SetMarketSyncCleanError verifies that a SetMarketSyncClean error
// is recorded in the result when DryRun=false.
func TestRefresh_SetMarketSyncCleanError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}}}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{
				"mkt": {LastSyncedSHA: "oldhash"},
			}}, nil
		},
		SetMarketSyncDirtyFn: func(cacheDir, market string) error { return nil },
		SetMarketSyncCleanFn: func(cacheDir, market, newSHA string) error {
			return errors.New("clean failed")
		},
	}
	git := &gitrepotest.MockGitRepo{
		FetchFn: func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return nil, nil
		},
	}
	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, state)
	results, err := app.Refresh(service.RefreshOpts{DryRun: false})
	if err != nil {
		t.Fatal("unexpected top-level error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil || results[0].Err.Error() != "clean failed" {
		t.Errorf("expected 'clean failed' error, got %v", results[0].Err)
	}
	if results[0].NewSHA != "newhash" {
		t.Errorf("expected NewSHA=newhash, got %q", results[0].NewSHA)
	}
}

// TestRefresh_NewMarketSkipsDiff verifies that when oldSHA is empty (new market,
// no previous sync), the diff step is skipped and ChangedFiles=0.
func TestRefresh_NewMarketSkipsDiff(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}}}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{}}, nil
		},
	}
	diffCalled := false
	git := &gitrepotest.MockGitRepo{
		FetchFn: func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			diffCalled = true
			return nil, nil
		},
	}
	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, state)
	results, err := app.Refresh(service.RefreshOpts{DryRun: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if diffCalled {
		t.Error("DiffSinceCommit should NOT be called when oldSHA is empty")
	}
	if results[0].ChangedFiles != 0 {
		t.Errorf("expected ChangedFiles=0, got %d", results[0].ChangedFiles)
	}
	if results[0].OldSHA != "" {
		t.Errorf("expected OldSHA=\"\", got %q", results[0].OldSHA)
	}
	if results[0].NewSHA != "newhash" {
		t.Errorf("expected NewSHA=newhash, got %q", results[0].NewSHA)
	}
}

// TestRefresh_DiffSinceCommitError verifies that a DiffSinceCommit error is
// silently handled (continues with 0 changed files).
func TestRefresh_DiffSinceCommitError(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: []domain.MarketConfig{{Name: "mkt", Branch: "main"}}}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{
				"mkt": {LastSyncedSHA: "oldhash"},
			}}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		FetchFn: func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return nil, errors.New("diff broken")
		},
	}
	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, state)
	results, err := app.Refresh(service.RefreshOpts{DryRun: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("expected no error in result (diff error is silent), got %v", results[0].Err)
	}
	if results[0].ChangedFiles != 0 {
		t.Errorf("expected ChangedFiles=0 after diff error, got %d", results[0].ChangedFiles)
	}
}

// TestRefresh_SkillsOnlyFiltering verifies that SkillsOnly markets filter
// non-skill paths from the changed files count.
func TestRefresh_SkillsOnlyFiltering(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: []domain.MarketConfig{
				{Name: "mkt", Branch: "main", SkillsOnly: true, SkillsPath: "skills"},
			}}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{
				"mkt": {LastSyncedSHA: "oldhash"},
			}}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		FetchFn: func(clonePath, branch string) (string, error) { return "newhash", nil },
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return []domain.FileDiff{
				{Action: domain.DiffModify, From: "skills/foo.md", To: "skills/foo.md"},
				{Action: domain.DiffModify, From: "agents/bar.md", To: "agents/bar.md"},
				{Action: domain.DiffModify, From: "README.md", To: "README.md"},
			}, nil
		},
	}
	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, state)
	results, err := app.Refresh(service.RefreshOpts{DryRun: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ChangedFiles != 1 {
		t.Errorf("expected ChangedFiles=1 (only skill path), got %d", results[0].ChangedFiles)
	}
}

// TestRefresh_MarketNameFiltering verifies that opts.Market filters markets by name.
func TestRefresh_MarketNameFiltering(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{Markets: []domain.MarketConfig{
				{Name: "alpha", Branch: "main"},
				{Name: "beta", Branch: "main"},
			}}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{}}, nil
		},
	}
	fetchedMarkets := []string{}
	git := &gitrepotest.MockGitRepo{
		FetchFn: func(clonePath, branch string) (string, error) {
			fetchedMarkets = append(fetchedMarkets, clonePath)
			return "newhash", nil
		},
	}
	app := newTestApp(cfg, git, &filesystemtest.MockFilesystem{}, state)
	results, err := app.Refresh(service.RefreshOpts{DryRun: true, Market: "beta"})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (only beta), got %d", len(results))
	}
	if results[0].Market != "beta" {
		t.Errorf("expected Market=beta, got %q", results[0].Market)
	}
}

// ---------------------------------------------------------------------------
// Sync
// ---------------------------------------------------------------------------

// TestSync_CallsRefreshAndUpdate verifies that Sync calls Refresh then Update.
func TestSync_CallsRefreshAndUpdate(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{}
	git := &gitrepotest.MockGitRepo{}
	fsMock := &filesystemtest.MockFilesystem{}
	state := &statestoretest.MockStateStore{}

	cfg.LoadFn = func(path string) (domain.Config, error) {
		return domain.Config{
			LocalPath: ".claude",
			Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
		}, nil
	}
	state.LoadSyncStateFn = func(cacheDir string) (domain.SyncState, error) {
		return domain.SyncState{
			Version: 1,
			Markets: map[string]domain.MarketSyncState{
				"mkt": {LastSyncedSHA: "oldhash"},
			},
		}, nil
	}
	git.FetchFn = func(clonePath, branch string) (string, error) {
		return "newhash", nil
	}
	git.DiffSinceCommitFn = func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
		return []domain.FileDiff{
			{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
		}, nil
	}
	state.SetMarketSyncDirtyFn = func(cacheDir, market string) error { return nil }
	state.SetMarketSyncCleanFn = func(cacheDir, market, newSHA string) error { return nil }

	app := newTestApp(cfg, git, fsMock, state)
	results, err := app.Sync(service.SyncOpts{})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 SyncResult, got %d", len(results))
	}
	r := results[0]
	if r.Refresh.Market != "mkt" {
		t.Errorf("expected Refresh.Market=mkt, got %q", r.Refresh.Market)
	}
	if r.Refresh.NewSHA != "newhash" {
		t.Errorf("expected Refresh.NewSHA=newhash, got %q", r.Refresh.NewSHA)
	}
}

func TestCommandFileRepoPath(t *testing.T) {
	a := &App{}
	cases := []struct {
		profile, command, want string
	}{
		{"dev/go", "foo.md", "dev/go/commands/foo.md"},
		{"", "foo.md", "commands/foo.md"},
	}
	for _, tc := range cases {
		got := a.commandFileRepoPath(tc.profile, tc.command)
		if got != tc.want {
			t.Errorf("commandFileRepoPath(%q, %q) = %q, want %q", tc.profile, tc.command, got, tc.want)
		}
	}
}

func TestPackageFileRefs_IncludesCommands(t *testing.T) {
	a := &App{}
	pkg := domain.InstalledPackage{
		Profile: "dev/go",
		Files: domain.InstalledFiles{
			Skills:   []string{"s1"},
			Agents:   []string{"a1.md"},
			Commands: []string{"c1.md"},
		},
	}
	refs := a.packageFileRefs("market", pkg)

	want := map[string]bool{
		"market@dev/go/skills/s1/SKILL.md": true,
		"market@dev/go/agents/a1.md":       true,
		"market@dev/go/commands/c1.md":     true,
	}
	for _, r := range refs {
		delete(want, string(r))
	}
	if len(want) > 0 {
		t.Errorf("missing refs: %v (got %v)", want, refs)
	}
}

func TestUpdate_AllLocationsFalse_FiltersLocations(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
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
				{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return []byte("new content"), nil
			}
			return nil, errors.New("not found at ref")
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "newhash", nil
		},
	}

	var writtenPaths []string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writtenPaths = append(writtenPaths, path)
			return nil
		},
		DeleteFileFn: func(path string) error { return nil },
	}

	currentProject := testProjectPath()

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{
				Market: "mkt",
				Packages: []domain.InstalledPackage{
					{
						Profile: "agents/foo.md",
						Version: "oldhash",
						Files:   domain.InstalledFiles{Agents: []string{"foo.md"}},
						Locations: []domain.InstalledLocation{
							{Path: currentProject, Type: domain.RuntimeTypeClaudeCode},
							{Path: "/other/project", Type: domain.RuntimeTypeClaudeCode},
						},
					},
				},
			},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)

	results, err := app.Update(service.UpdateOpts{AllLocations: false})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "update" {
		t.Errorf("expected Action=update, got %q", results[0].Action)
	}
	if results[0].Location != currentProject {
		t.Errorf("expected Location=%q (current project), got %q", currentProject, results[0].Location)
	}
	if len(writtenPaths) == 0 {
		t.Error("expected file writes, got none")
	}
	for _, p := range writtenPaths {
		if strings.Contains(p, "/other/project") {
			t.Errorf("wrote to wrong location: %q", p)
		}
	}
}

func TestUpdate_AllLocationsTrue_UpdatesAll(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
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
				{Action: domain.DiffModify, From: "agents/foo.md", To: "agents/foo.md"},
			}, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return []byte("new content"), nil
			}
			return nil, errors.New("not found at ref")
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "newhash", nil
		},
	}

	var writtenPaths []string
	fsMock := &filesystemtest.MockFilesystem{
		WriteFileFn: func(path string, content []byte) error {
			writtenPaths = append(writtenPaths, path)
			return nil
		},
		DeleteFileFn: func(path string) error { return nil },
	}

	currentProject := testProjectPath()

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{
				Market: "mkt",
				Packages: []domain.InstalledPackage{
					{
						Profile: "agents/foo.md",
						Version: "oldhash",
						Files:   domain.InstalledFiles{Agents: []string{"foo.md"}},
						Locations: []domain.InstalledLocation{
							{Path: currentProject, Type: domain.RuntimeTypeClaudeCode},
							{Path: "/other/project", Type: domain.RuntimeTypeClaudeCode},
						},
					},
				},
			},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)

	results, err := app.Update(service.UpdateOpts{AllLocations: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (both locations), got %d", len(results))
	}
	for _, r := range results {
		if r.Action != "update" {
			t.Errorf("expected Action=update for %q, got %q", r.Location, r.Action)
		}
	}
	hasCurrent := false
	hasOther := false
	for _, p := range writtenPaths {
		if strings.Contains(p, currentProject) {
			hasCurrent = true
		}
		if strings.Contains(p, "/other/project") {
			hasOther = true
		}
	}
	if !hasCurrent {
		t.Error("expected write to current project location")
	}
	if !hasOther {
		t.Error("expected write to /other/project location")
	}
}

func TestUpdate_DriftOnly_NoUpstream_AllMerge(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{
				Version: 1,
				Markets: map[string]domain.MarketSyncState{"mkt": {LastSyncedSHA: "oldhash"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return nil, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return []byte("content"), nil
			}
			return []byte("original"), nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "oldhash", nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		MD5ChecksumFn: func(content []byte) string { return "hash" },
		WriteFileFn:   func(path string, content []byte) error { return nil },
		DeleteFileFn:  func(path string) error { return nil },
	}

	db := installedDB("mkt", "agents/foo.md", "oldhash", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Update(service.UpdateOpts{AllMerge: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "update" {
		t.Errorf("expected Action=update, got %q", results[0].Action)
	}
}

func TestUpdate_DriftOnly_NoUpstream_AllKeep(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{
				Version: 1,
				Markets: map[string]domain.MarketSyncState{"mkt": {LastSyncedSHA: "oldhash"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return nil, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return []byte("content"), nil
			}
			return []byte("original"), nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		MD5ChecksumFn: func(content []byte) string { return "hash" },
	}

	db := installedDB("mkt", "agents/foo.md", "oldhash", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Update(service.UpdateOpts{AllKeep: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "kept" {
		t.Errorf("expected Action=kept, got %q", results[0].Action)
	}
	if len(results[0].DriftFiles) == 0 {
		t.Error("expected DriftFiles to be populated")
	}
}

func TestUpdate_DiffError_Drift_AllMerge(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{
				Version: 1,
				Markets: map[string]domain.MarketSyncState{"mkt": {LastSyncedSHA: "oldhash"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return nil, errors.New("shallow clone missing commit")
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return []byte("content"), nil
			}
			return []byte("original"), nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "oldhash", nil
		},
	}
	fsMock := &filesystemtest.MockFilesystem{
		MD5ChecksumFn: func(content []byte) string { return "hash" },
		WriteFileFn:   func(path string, content []byte) error { return nil },
		DeleteFileFn:  func(path string) error { return nil },
	}

	db := installedDB("mkt", "agents/foo.md", "oldhash", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Update(service.UpdateOpts{AllMerge: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "update" {
		t.Errorf("expected Action=update, got %q", results[0].Action)
	}
}

func TestUpdate_DriftAtOtherLocation_AllMerge(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{
				Version: 1,
				Markets: map[string]domain.MarketSyncState{"mkt": {LastSyncedSHA: "oldhash"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return nil, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			if commitSHA == "HEAD" {
				return []byte("content"), nil
			}
			return []byte("original"), nil
		},
		RemoteHEADFn: func(clonePath, branch string) (string, error) {
			return "oldhash", nil
		},
	}

	otherProject := "/other/project"
	var writtenPaths []string
	fsMock := &filesystemtest.MockFilesystem{
		MD5ChecksumFn: func(content []byte) string { return "hash" },
		WriteFileFn: func(path string, content []byte) error {
			writtenPaths = append(writtenPaths, path)
			return nil
		},
		DeleteFileFn: func(path string) error { return nil },
	}

	db := domain.InstallDatabase{
		Markets: []domain.InstalledMarket{
			{
				Market: "mkt",
				Packages: []domain.InstalledPackage{
					{
						Profile: "agents/foo.md",
						Version: "oldhash",
						Files:   domain.InstalledFiles{Agents: []string{"foo.md"}},
						Locations: []domain.InstalledLocation{
							{Path: otherProject, Type: domain.RuntimeTypeClaudeCode},
						},
					},
				},
			},
		},
	}
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Update(service.UpdateOpts{AllMerge: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (drifted location), got %d", len(results))
	}
	if results[0].Action != "update" {
		t.Errorf("expected Action=update, got %q", results[0].Action)
	}
	if results[0].Location != otherProject {
		t.Errorf("expected Location=%q, got %q", otherProject, results[0].Location)
	}
	wroteToOther := false
	for _, p := range writtenPaths {
		if strings.Contains(p, otherProject) {
			wroteToOther = true
			break
		}
	}
	if !wroteToOther {
		t.Error("expected files to be written to the other-project location")
	}
}

func TestUpdate_NoDriftNoUpstream_AllMerge_Noop(t *testing.T) {
	cfg := &configstoretest.MockConfigStore{
		LoadFn: func(path string) (domain.Config, error) {
			return domain.Config{
				LocalPath: ".claude",
				Markets:   []domain.MarketConfig{{Name: "mkt", Branch: "main"}},
			}, nil
		},
	}
	state := &statestoretest.MockStateStore{
		LoadSyncStateFn: func(cacheDir string) (domain.SyncState, error) {
			return domain.SyncState{
				Version: 1,
				Markets: map[string]domain.MarketSyncState{"mkt": {LastSyncedSHA: "oldhash"}},
			}, nil
		},
	}
	git := &gitrepotest.MockGitRepo{
		DiffSinceCommitFn: func(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
			return nil, nil
		},
		ReadFileAtRefFn: func(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
			return []byte("original"), nil
		},
	}

	currentProject := testProjectPath()
	fsMock := &filesystemtest.MockFilesystem{
		MD5ChecksumFn: func(content []byte) string { return "hash" },
		ReadFileFn: func(name string) ([]byte, error) {
			return []byte("original"), nil
		},
	}

	db := installedDB("mkt", "agents/foo.md", "oldhash", domain.InstalledFiles{Agents: []string{"foo.md"}})
	idb := idbWithData(db)

	app := newTestApp(cfg, git, fsMock, state, idb)
	results, err := app.Update(service.UpdateOpts{AllMerge: true})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for clean package, got %d: %+v", len(results), results)
	}
	_ = currentProject
}
