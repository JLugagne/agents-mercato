package commands

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// ---------------------------------------------------------------------------
// Stub types
// ---------------------------------------------------------------------------

type stubMarkets struct {
	listFn              func() ([]domain.Market, error)
	getMarketFn         func(name string) (domain.Market, error)
	marketInfoFn        func(name string) (service.MarketInfoResult, error)
	addMarketFn         func(url string, opts service.AddMarketOpts) (service.AddMarketResult, error)
	removeMarketFn      func(name string, opts service.RemoveMarketOpts) error
	setMarketPropertyFn func(name, key, value string) error
	lintMarketFn        func(fsys fs.FS, dir string) (service.LintResult, error)
}

func (s *stubMarkets) ListMarkets() ([]domain.Market, error) {
	if s.listFn != nil {
		return s.listFn()
	}
	return nil, nil
}

func (s *stubMarkets) GetMarket(name string) (domain.Market, error) {
	if s.getMarketFn != nil {
		return s.getMarketFn(name)
	}
	return domain.Market{}, nil
}

func (s *stubMarkets) MarketInfo(name string) (service.MarketInfoResult, error) {
	if s.marketInfoFn != nil {
		return s.marketInfoFn(name)
	}
	return service.MarketInfoResult{}, nil
}

func (s *stubMarkets) AddMarket(url string, opts service.AddMarketOpts) (service.AddMarketResult, error) {
	if s.addMarketFn != nil {
		return s.addMarketFn(url, opts)
	}
	return service.AddMarketResult{}, nil
}

func (s *stubMarkets) RemoveMarket(name string, opts service.RemoveMarketOpts) error {
	if s.removeMarketFn != nil {
		return s.removeMarketFn(name, opts)
	}
	return nil
}

func (s *stubMarkets) SetMarketProperty(name, key, value string) error {
	if s.setMarketPropertyFn != nil {
		return s.setMarketPropertyFn(name, key, value)
	}
	return nil
}

func (s *stubMarkets) LintMarket(fsys fs.FS, dir string) (service.LintResult, error) {
	if s.lintMarketFn != nil {
		return s.lintMarketFn(fsys, dir)
	}
	return service.LintResult{}, nil
}

// ---------------------------------------------------------------------------

type stubSync struct {
	checkFn          func(opts service.CheckOpts) ([]domain.EntryStatus, error)
	syncStateFn      func() (domain.SyncState, error)
	refreshFn        func(opts service.RefreshOpts) ([]service.RefreshResult, error)
	updateFn         func(opts service.UpdateOpts) ([]service.UpdateResult, error)
	syncFn           func(opts service.SyncOpts) ([]service.SyncResult, error)
	detectDeletedFn  func(opts service.DetectDeletedOpts) ([]service.DeletedFile, error)
	restoreDeletedFn func(files []service.DeletedFile, opts service.RestoreOpts) ([]service.RestoreResult, error)
	migrateFn        func(opts service.MigrateOpts) ([]service.MigrateResult, error)
}

func (s *stubSync) Check(opts service.CheckOpts) ([]domain.EntryStatus, error) {
	if s.checkFn != nil {
		return s.checkFn(opts)
	}
	return nil, nil
}

func (s *stubSync) SyncState() (domain.SyncState, error) {
	if s.syncStateFn != nil {
		return s.syncStateFn()
	}
	return domain.SyncState{}, nil
}

func (s *stubSync) Refresh(opts service.RefreshOpts) ([]service.RefreshResult, error) {
	if s.refreshFn != nil {
		return s.refreshFn(opts)
	}
	return nil, nil
}

func (s *stubSync) Update(opts service.UpdateOpts) ([]service.UpdateResult, error) {
	if s.updateFn != nil {
		return s.updateFn(opts)
	}
	return nil, nil
}

func (s *stubSync) Sync(opts service.SyncOpts) ([]service.SyncResult, error) {
	if s.syncFn != nil {
		return s.syncFn(opts)
	}
	return nil, nil
}

func (s *stubSync) DetectDeleted(opts service.DetectDeletedOpts) ([]service.DeletedFile, error) {
	if s.detectDeletedFn != nil {
		return s.detectDeletedFn(opts)
	}
	return nil, nil
}

func (s *stubSync) Migrate(opts service.MigrateOpts) ([]service.MigrateResult, error) {
	if s.migrateFn != nil {
		return s.migrateFn(opts)
	}
	return nil, nil
}

func (s *stubSync) RestoreDeleted(files []service.DeletedFile, opts service.RestoreOpts) ([]service.RestoreResult, error) {
	if s.restoreDeletedFn != nil {
		return s.restoreDeletedFn(files, opts)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------

type stubEntries struct {
	listFn             func(opts service.ListOpts) ([]domain.Entry, error)
	getEntryFn         func(ref domain.MctRef) (domain.Entry, error)
	readEntryContentFn func(market, relPath string) ([]byte, error)
	conflictsFn        func() ([]domain.Conflict, error)
	addFn              func(ref domain.MctRef, opts service.AddOpts) (service.AddResult, error)
	removeFn           func(ref domain.MctRef) (service.RemoveResult, error)
	pruneFn            func(opts service.PruneOpts) ([]service.PruneResult, error)
	diffFn             func(ref domain.MctRef) error
	initFn             func(opts service.InitOpts) error
}

func (s *stubEntries) List(opts service.ListOpts) ([]domain.Entry, error) {
	if s.listFn != nil {
		return s.listFn(opts)
	}
	return nil, nil
}

func (s *stubEntries) GetEntry(ref domain.MctRef) (domain.Entry, error) {
	if s.getEntryFn != nil {
		return s.getEntryFn(ref)
	}
	return domain.Entry{}, nil
}

func (s *stubEntries) ReadEntryContent(market, relPath string) ([]byte, error) {
	if s.readEntryContentFn != nil {
		return s.readEntryContentFn(market, relPath)
	}
	return nil, nil
}

func (s *stubEntries) ListSkillDirFiles(market, dirPrefix string) ([]domain.SkillDirFile, error) {
	return nil, nil
}

func (s *stubEntries) Conflicts() ([]domain.Conflict, error) {
	if s.conflictsFn != nil {
		return s.conflictsFn()
	}
	return nil, nil
}

func (s *stubEntries) Add(ref domain.MctRef, opts service.AddOpts) (service.AddResult, error) {
	if s.addFn != nil {
		return s.addFn(ref, opts)
	}
	return service.AddResult{}, nil
}

func (s *stubEntries) Remove(ref domain.MctRef, opts service.RemoveOpts) (service.RemoveResult, error) {
	if s.removeFn != nil {
		return s.removeFn(ref)
	}
	return service.RemoveResult{}, nil
}

func (s *stubEntries) Prune(opts service.PruneOpts) ([]service.PruneResult, error) {
	if s.pruneFn != nil {
		return s.pruneFn(opts)
	}
	return nil, nil
}

func (s *stubEntries) Diff(ref domain.MctRef) error {
	if s.diffFn != nil {
		return s.diffFn(ref)
	}
	return nil
}

func (s *stubEntries) Init(opts service.InitOpts) error {
	if s.initFn != nil {
		return s.initFn(opts)
	}
	return nil
}

// ---------------------------------------------------------------------------

type stubSearch struct {
	searchFn     func(query string, opts service.SearchOpts) ([]service.SearchResult, error)
	dumpIndexFn  func() ([]domain.Entry, error)
	benchIndexFn func() (service.BenchResult, error)
}

func (s *stubSearch) Search(query string, opts service.SearchOpts) ([]service.SearchResult, error) {
	if s.searchFn != nil {
		return s.searchFn(query, opts)
	}
	return nil, nil
}

func (s *stubSearch) DumpIndex() ([]domain.Entry, error) {
	if s.dumpIndexFn != nil {
		return s.dumpIndexFn()
	}
	return nil, nil
}

func (s *stubSearch) BenchIndex() (service.BenchResult, error) {
	if s.benchIndexFn != nil {
		return s.benchIndexFn()
	}
	return service.BenchResult{}, nil
}

// ---------------------------------------------------------------------------

type stubReadmes struct {
	readmeFn      func(market, path string) (domain.ReadmeEntry, error)
	listReadmesFn func(market string) ([]domain.ReadmeEntry, error)
}

func (s *stubReadmes) Readme(market, path string) (domain.ReadmeEntry, error) {
	if s.readmeFn != nil {
		return s.readmeFn(market, path)
	}
	return domain.ReadmeEntry{}, nil
}

func (s *stubReadmes) ListReadmes(market string) ([]domain.ReadmeEntry, error) {
	if s.listReadmesFn != nil {
		return s.listReadmesFn(market)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------

type stubConfig struct {
	setConfigFieldFn func(key, value string) error
	getConfigFn      func() (domain.Config, error)
}

func (s *stubConfig) SetConfigField(key, value string) error {
	if s.setConfigFieldFn != nil {
		return s.setConfigFieldFn(key, value)
	}
	return nil
}

func (s *stubConfig) GetConfig() (domain.Config, error) {
	if s.getConfigFn != nil {
		return s.getConfigFn()
	}
	return domain.Config{}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mockServices() Services {
	return Services{
		Markets: &stubMarkets{},
		Sync:    &stubSync{},
		Entries: &stubEntries{},
		Search:  &stubSearch{},
		Readmes: &stubReadmes{},
		Config:  &stubConfig{},
		Doctor:  &stubDoctor{},
	}
}

type stubDoctor struct {
	doctorFn func(opts service.DoctorOpts) (service.DoctorReport, error)
}

func (s *stubDoctor) Doctor(opts service.DoctorOpts) (service.DoctorReport, error) {
	if s.doctorFn != nil {
		return s.doctorFn(opts)
	}
	return service.DoctorReport{}, nil
}

func runCmd(t *testing.T, svc Services, args ...string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	root := NewRootCmd(svc)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func runCmdWithStdin(t *testing.T, svc Services, stdin string, args ...string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	root := NewRootCmd(svc)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func mustValidJSON(t *testing.T, s string) {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("expected valid JSON, got error: %v\noutput: %s", err, s)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestMarketList_Text(t *testing.T) {
	svc := mockServices()
	svc.Markets = &stubMarkets{
		listFn: func() ([]domain.Market, error) {
			return []domain.Market{
				{Name: "alpha", URL: "https://a.com", Branch: "main", ReadOnly: false},
				{Name: "beta", URL: "https://b.com", Branch: "main", ReadOnly: true},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "market", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected output to contain 'alpha', got: %s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("expected output to contain 'beta', got: %s", out)
	}
	if !strings.Contains(out, "●") {
		t.Errorf("expected output to contain '●' (rw indicator), got: %s", out)
	}
	if !strings.Contains(out, "○") {
		t.Errorf("expected output to contain '○' (ro indicator), got: %s", out)
	}
}

func TestMarketList_JSON(t *testing.T) {
	svc := mockServices()
	svc.Markets = &stubMarkets{
		listFn: func() ([]domain.Market, error) {
			return []domain.Market{
				{Name: "alpha", URL: "https://a.com", Branch: "main"},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "market", "list", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected JSON to contain 'alpha', got: %s", out)
	}
}

func TestMarketAdd(t *testing.T) {
	var calledURL string
	svc := mockServices()
	svc.Markets = &stubMarkets{
		addMarketFn: func(url string, opts service.AddMarketOpts) (service.AddMarketResult, error) {
			calledURL = url
			return service.AddMarketResult{Profiles: 2, Agents: 5, Skills: 3}, nil
		},
	}
	out, err := runCmd(t, svc, "market", "add", "https://github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledURL != "https://github.com/org/repo" {
		t.Errorf("expected addMarket called with url 'https://github.com/org/repo', got: %s", calledURL)
	}
	if !strings.Contains(out, "2 profiles") {
		t.Errorf("expected output to contain '2 profiles', got: %s", out)
	}
	if !strings.Contains(out, "5 agents") {
		t.Errorf("expected output to contain '5 agents', got: %s", out)
	}
	if !strings.Contains(out, "3 skills") {
		t.Errorf("expected output to contain '3 skills', got: %s", out)
	}
}

func TestMarketAdd_NoScheme(t *testing.T) {
	var calledURL string
	svc := mockServices()
	svc.Markets = &stubMarkets{
		addMarketFn: func(url string, opts service.AddMarketOpts) (service.AddMarketResult, error) {
			calledURL = url
			return service.AddMarketResult{}, nil
		},
	}
	_, err := runCmd(t, svc, "market", "add", "github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledURL != "https://github.com/org/repo" {
		t.Errorf("expected https:// prefix, got: %s", calledURL)
	}
}

func TestMarketAdd_SSHUrl(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"scp-style", "git@gitlab.intdw.org:common/ai-rules.git"},
		{"scp-github", "git@github.com:org/repo.git"},
		{"ssh-scheme", "ssh://git@github.com/org/repo.git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calledURL string
			svc := mockServices()
			svc.Markets = &stubMarkets{
				addMarketFn: func(url string, opts service.AddMarketOpts) (service.AddMarketResult, error) {
					calledURL = url
					return service.AddMarketResult{}, nil
				},
			}
			_, err := runCmd(t, svc, "market", "add", tc.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if calledURL != tc.url {
				t.Errorf("expected URL to be passed through unchanged %q, got: %q", tc.url, calledURL)
			}
		})
	}
}

func TestMarketAdd_JSON(t *testing.T) {
	svc := mockServices()
	svc.Markets = &stubMarkets{
		addMarketFn: func(url string, opts service.AddMarketOpts) (service.AddMarketResult, error) {
			return service.AddMarketResult{Profiles: 2, Agents: 5, Skills: 3}, nil
		},
	}
	out, err := runCmd(t, svc, "market", "add", "https://github.com/org/repo", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if _, ok := m["profiles"]; !ok {
		t.Errorf("expected JSON to have 'profiles' key, got: %s", out)
	}
	if _, ok := m["agents"]; !ok {
		t.Errorf("expected JSON to have 'agents' key, got: %s", out)
	}
	if _, ok := m["skills"]; !ok {
		t.Errorf("expected JSON to have 'skills' key, got: %s", out)
	}
}

func TestMarketAdd_Error(t *testing.T) {
	svc := mockServices()
	svc.Markets = &stubMarkets{
		addMarketFn: func(url string, opts service.AddMarketOpts) (service.AddMarketResult, error) {
			return service.AddMarketResult{}, &domain.DomainError{Code: "ERR", Message: "some error"}
		},
	}
	_, err := runCmd(t, svc, "market", "add", "https://github.com/org/repo")
	if err == nil {
		t.Fatal("expected non-nil error, got nil")
	}
}

func TestMarketInfo(t *testing.T) {
	svc := mockServices()
	svc.Markets = &stubMarkets{
		marketInfoFn: func(name string) (service.MarketInfoResult, error) {
			return service.MarketInfoResult{
				Market:         domain.Market{Name: "foo", URL: "https://foo.com", Branch: "main"},
				EntryCount:     10,
				InstalledCount: 3,
				Status:         "clean",
			}, nil
		},
	}
	out, err := runCmd(t, svc, "market", "info", "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"foo", "10", "3", "clean"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got: %s", want, out)
		}
	}
}

func TestMarketSet(t *testing.T) {
	var calledName, calledKey, calledValue string
	svc := mockServices()
	svc.Markets = &stubMarkets{
		setMarketPropertyFn: func(name, key, value string) error {
			calledName = name
			calledKey = key
			calledValue = value
			return nil
		},
	}
	out, err := runCmd(t, svc, "market", "set", "foo", "branch", "develop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledName != "foo" || calledKey != "branch" || calledValue != "develop" {
		t.Errorf("expected set called with ('foo', 'branch', 'develop'), got ('%s', '%s', '%s')", calledName, calledKey, calledValue)
	}
	if !strings.Contains(out, "foo.branch = develop") {
		t.Errorf("expected output to contain 'foo.branch = develop', got: %s", out)
	}
}

func TestMarketRemove(t *testing.T) {
	var calledName string
	var calledOpts service.RemoveMarketOpts
	svc := mockServices()
	svc.Markets = &stubMarkets{
		removeMarketFn: func(name string, opts service.RemoveMarketOpts) error {
			calledName = name
			calledOpts = opts
			return nil
		},
	}
	_, err := runCmd(t, svc, "market", "remove", "mymarket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledName != "mymarket" {
		t.Errorf("expected remove called with 'mymarket', got: %s", calledName)
	}
	_ = calledOpts
}

func TestRefresh_Text(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		refreshFn: func(opts service.RefreshOpts) ([]service.RefreshResult, error) {
			return []service.RefreshResult{
				{Market: "mkt", OldSHA: "abc1234xxx", NewSHA: "def5678xxx", ChangedFiles: 3},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "refresh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "mkt") {
		t.Errorf("expected output to contain 'mkt', got: %s", out)
	}
	if !strings.Contains(out, "def5678") {
		t.Errorf("expected output to contain 'def5678', got: %s", out)
	}
}

func TestCheck_Text(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		checkFn: func(opts service.CheckOpts) ([]domain.EntryStatus, error) {
			return []domain.EntryStatus{
				{Ref: "mkt@agents/foo.md", State: domain.StateClean},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "check")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected output to contain 'ok', got: %s", out)
	}
	if !strings.Contains(out, "mkt@agents/foo.md") {
		t.Errorf("expected output to contain 'mkt/agents/foo.md', got: %s", out)
	}
}

func TestAdd_Text(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		addFn: func(ref domain.MctRef, opts service.AddOpts) (service.AddResult, error) {
			return service.AddResult{}, nil
		},
	}
	out, err := runCmd(t, svc, "add", "mkt@agents/foo.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "installed") {
		t.Errorf("expected output to contain 'installed', got: %s", out)
	}
	if !strings.Contains(out, "mkt@agents/foo.md") {
		t.Errorf("expected output to contain 'mkt/agents/foo.md', got: %s", out)
	}
}

func TestAdd_JSON(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		addFn: func(ref domain.MctRef, opts service.AddOpts) (service.AddResult, error) {
			return service.AddResult{}, nil
		},
	}
	out, err := runCmd(t, svc, "add", "mkt@agents/foo.md", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["status"] != "installed" {
		t.Errorf("expected status 'installed', got: %v", m["status"])
	}
}

func TestRemove_Text(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		removeFn: func(ref domain.MctRef) (service.RemoveResult, error) {
			return service.RemoveResult{}, nil
		},
	}
	out, err := runCmd(t, svc, "remove", "--ref", "mkt@agents/foo.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("expected output to contain 'removed', got: %s", out)
	}
}

func TestList_Text(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		listFn: func(opts service.ListOpts) ([]domain.Entry, error) {
			return []domain.Entry{
				{Ref: "mkt@profile/sub/agents/foo.md", Market: "mkt", Type: domain.EntryTypeAgent, Installed: true},
				{Ref: "mkt@profile/sub/skills/bar.md", Market: "mkt", Type: domain.EntryTypeSkill, Installed: true},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "mkt") {
		t.Errorf("expected output to contain 'mkt', got: %s", out)
	}
	if !strings.Contains(out, "mkt@profile/sub/agents/foo.md") {
		t.Errorf("expected output to contain ref 'mkt/profile/sub/agents/foo.md', got: %s", out)
	}
	if !strings.Contains(out, "mkt@profile/sub/skills/bar.md") {
		t.Errorf("expected output to contain ref 'mkt/profile/sub/skills/bar.md', got: %s", out)
	}
}

func TestList_JSON(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		listFn: func(opts service.ListOpts) ([]domain.Entry, error) {
			return []domain.Entry{
				{Ref: "mkt@profile/sub/agents/foo.md", Market: "mkt", Type: domain.EntryTypeAgent, Installed: true},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "list", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
	var arr []any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("expected JSON array, got error: %v", err)
	}
}

func TestSearch_Text(t *testing.T) {
	svc := mockServices()
	svc.Search = &stubSearch{
		searchFn: func(query string, opts service.SearchOpts) ([]service.SearchResult, error) {
			return []service.SearchResult{
				{
					Entry: domain.Entry{
						Market:      "mkt",
						Category:    "dev/go",
						Description: "Go agent",
						Installed:   false,
					},
					Score: 1.5,
				},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "search", "golang")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "mkt@dev/go") {
		t.Errorf("expected output to contain 'mkt/dev/go', got: %s", out)
	}
}

func TestSearch_JSON(t *testing.T) {
	svc := mockServices()
	svc.Search = &stubSearch{
		searchFn: func(query string, opts service.SearchOpts) ([]service.SearchResult, error) {
			return []service.SearchResult{
				{
					Entry: domain.Entry{Market: "mkt", Category: "dev/go"},
					Score: 1.0,
				},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "search", "golang", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
}

func TestConflicts_NoConflicts(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		conflictsFn: func() ([]domain.Conflict, error) {
			return nil, nil
		},
	}
	out, err := runCmd(t, svc, "conflicts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No conflicts") {
		t.Errorf("expected output to contain 'No conflicts', got: %s", out)
	}
}

func TestConflicts_WithConflicts(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		conflictsFn: func() ([]domain.Conflict, error) {
			return []domain.Conflict{
				{Type: "ref-collision", Description: "foo", Severity: "error"},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "conflicts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "ref-collision") {
		t.Errorf("expected output to contain 'ref-collision', got: %s", out)
	}
}

func TestSyncState_JSON(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		syncStateFn: func() (domain.SyncState, error) {
			return domain.SyncState{
				Version: 1,
				Markets: map[string]domain.MarketSyncState{
					"mkt": {LastSyncedSHA: "abc1234567", Status: "clean", LastSyncedAt: time.Now()},
				},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "sync-state", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
}

func TestConfigGet_Text(t *testing.T) {
	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{
				LocalPath:      ".claude",
				ConflictPolicy: "block",
			}, nil
		},
	}
	out, err := runCmd(t, svc, "config", "get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "local_path") {
		t.Errorf("expected output to contain 'local_path', got: %s", out)
	}
	if !strings.Contains(out, ".claude") {
		t.Errorf("expected output to contain '.claude', got: %s", out)
	}
}

func TestConfigGet_JSON(t *testing.T) {
	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{LocalPath: ".claude", ConflictPolicy: "block"}, nil
		},
	}
	out, err := runCmd(t, svc, "config", "get", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
}

func TestConfigSet(t *testing.T) {
	var calledKey, calledValue string
	svc := mockServices()
	svc.Config = &stubConfig{
		setConfigFieldFn: func(key, value string) error {
			calledKey = key
			calledValue = value
			return nil
		},
	}
	out, err := runCmd(t, svc, "config", "set", "ssh_enabled", "true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledKey != "ssh_enabled" || calledValue != "true" {
		t.Errorf("expected set called with ('ssh_enabled', 'true'), got ('%s', '%s')", calledKey, calledValue)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected output to contain 'ok', got: %s", out)
	}
}

func TestExportToStdout(t *testing.T) {
	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{
					{Name: "mkt", URL: "https://mkt.com", Branch: "main"},
				},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "export")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	for _, key := range []string{"version", "markets", "entries"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON to have key %q, got: %s", key, out)
		}
	}
}

func TestExportToFile(t *testing.T) {
	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{
					{Name: "mkt", URL: "https://mkt.com", Branch: "main"},
				},
			}, nil
		},
	}
	tmpFile := "/tmp/test-export-mct.json"
	defer func() { _ = os.Remove(tmpFile) }()

	_, err := runCmd(t, svc, "export", tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	mustValidJSON(t, string(raw))
}

func TestImport_DryRun(t *testing.T) {
	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{}, nil
		},
	}
	_, err := runCmd(t, svc, "import", "/some/nonexistent/file.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLint_NoIssues(t *testing.T) {
	svc := mockServices()
	svc.Markets = &stubMarkets{
		lintMarketFn: func(fsys fs.FS, dir string) (service.LintResult, error) {
			return service.LintResult{Profiles: 1, Agents: 2, Skills: 1, Issues: nil}, nil
		},
	}
	out, err := runCmd(t, svc, "lint", ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "profiles: 1") {
		t.Errorf("expected output to contain 'profiles: 1', got: %s", out)
	}
}

func TestLint_WithErrors(t *testing.T) {
	svc := mockServices()
	svc.Markets = &stubMarkets{
		lintMarketFn: func(fsys fs.FS, dir string) (service.LintResult, error) {
			return service.LintResult{
				Issues: []service.LintIssue{
					{Profile: "p", Severity: "error", Message: "bad"},
				},
			}, nil
		},
	}
	_, err := runCmd(t, svc, "lint", ".")
	if err == nil {
		t.Fatal("expected non-nil error for lint errors, got nil")
	}
}

func TestReadme_Show(t *testing.T) {
	svc := mockServices()
	svc.Readmes = &stubReadmes{
		readmeFn: func(market, path string) (domain.ReadmeEntry, error) {
			return domain.ReadmeEntry{
				Market:  "mkt",
				Path:    "README.md",
				Content: "# Hello\n",
			}, nil
		},
	}
	out, err := runCmd(t, svc, "readme", "mkt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "mkt/README.md") {
		t.Errorf("expected output to contain 'mkt/README.md', got: %s", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Errorf("expected output to contain 'Hello', got: %s", out)
	}
}

func TestReadme_List(t *testing.T) {
	svc := mockServices()
	svc.Readmes = &stubReadmes{
		listReadmesFn: func(market string) ([]domain.ReadmeEntry, error) {
			return []domain.ReadmeEntry{
				{Market: "mkt", Path: "README.md"},
				{Market: "mkt", Path: "agents/README.md"},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "readme", "mkt", "--list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "mkt/README.md") {
		t.Errorf("expected output to contain 'mkt/README.md', got: %s", out)
	}
	if !strings.Contains(out, "mkt/agents/README.md") {
		t.Errorf("expected output to contain 'mkt/agents/README.md', got: %s", out)
	}
}

func TestVersionFlag(t *testing.T) {
	svc := mockServices()
	_, err := runCmd(t, svc, "--version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMarketsAlias(t *testing.T) {
	svc := mockServices()
	svc.Markets = &stubMarkets{
		listFn: func() ([]domain.Market, error) {
			return []domain.Market{
				{Name: "alpha", URL: "https://a.com", Branch: "main", ReadOnly: false},
				{Name: "beta", URL: "https://b.com", Branch: "main", ReadOnly: true},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "markets")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected output to contain 'alpha', got: %s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("expected output to contain 'beta', got: %s", out)
	}
}

func TestStatusAlias(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		checkFn: func(opts service.CheckOpts) ([]domain.EntryStatus, error) {
			return []domain.EntryStatus{
				{Ref: "mkt@agents/foo.md", State: domain.StateClean},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected output to contain 'ok', got: %s", out)
	}
	if !strings.Contains(out, "mkt@agents/foo.md") {
		t.Errorf("expected output to contain 'mkt/agents/foo.md', got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

func TestUpdateCmd_Default(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		updateFn: func(opts service.UpdateOpts) ([]service.UpdateResult, error) {
			return []service.UpdateResult{
				{
					Ref:        "mkt@agents/foo.md",
					Action:     "update",
					OldVersion: "v1",
					NewVersion: "v2",
				},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "update")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "update") {
		t.Errorf("expected output to contain 'update', got: %s", out)
	}
	if !strings.Contains(out, "mkt@agents/foo.md") {
		t.Errorf("expected output to contain ref, got: %s", out)
	}
}

func TestUpdateCmd_JSON(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		updateFn: func(opts service.UpdateOpts) ([]service.UpdateResult, error) {
			return []service.UpdateResult{
				{Ref: "mkt@agents/foo.md", Action: "update", OldVersion: "v1", NewVersion: "v2"},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "update", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
	var arr []any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("expected JSON array, got error: %v", err)
	}
}

func TestUpdateCmd_Error(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		updateFn: func(opts service.UpdateOpts) ([]service.UpdateResult, error) {
			return nil, &domain.DomainError{Code: "ERR", Message: "update failed"}
		},
	}
	_, err := runCmd(t, svc, "update")
	if err == nil {
		t.Fatal("expected non-nil error, got nil")
	}
}

// ---------------------------------------------------------------------------
// sync command
// ---------------------------------------------------------------------------

func TestSyncCmd_Default(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		refreshFn: func(opts service.RefreshOpts) ([]service.RefreshResult, error) {
			return []service.RefreshResult{
				{Market: "mkt", OldSHA: "aaaaaaa0000", NewSHA: "bbbbbbb1111"},
			}, nil
		},
		updateFn: func(opts service.UpdateOpts) ([]service.UpdateResult, error) {
			return []service.UpdateResult{
				{Ref: "mkt@agents/bar.md", Action: "update"},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "sync")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "mkt") {
		t.Errorf("expected output to contain 'mkt', got: %s", out)
	}
}

func TestSyncCmd_JSON(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		refreshFn: func(opts service.RefreshOpts) ([]service.RefreshResult, error) {
			return []service.RefreshResult{
				{Market: "mkt", OldSHA: "aaaaaaa0000", NewSHA: "bbbbbbb1111"},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "sync", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
}

func TestSyncCmd_DryRun(t *testing.T) {
	var refreshDryRun, updateDryRun bool
	svc := mockServices()
	svc.Sync = &stubSync{
		refreshFn: func(opts service.RefreshOpts) ([]service.RefreshResult, error) {
			refreshDryRun = opts.DryRun
			return nil, nil
		},
		updateFn: func(opts service.UpdateOpts) ([]service.UpdateResult, error) {
			updateDryRun = opts.DryRun
			return nil, nil
		},
	}
	_, err := runCmd(t, svc, "sync", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !refreshDryRun || !updateDryRun {
		t.Errorf("expected DryRun=true on both Refresh and Update, got refresh=%v update=%v", refreshDryRun, updateDryRun)
	}
}

// ---------------------------------------------------------------------------
// remove command
// ---------------------------------------------------------------------------

func TestRemoveCmd_Success(t *testing.T) {
	var calledRef domain.MctRef
	svc := mockServices()
	svc.Entries = &stubEntries{
		removeFn: func(ref domain.MctRef) (service.RemoveResult, error) {
			calledRef = ref
			return service.RemoveResult{}, nil
		},
	}
	out, err := runCmd(t, svc, "remove", "--ref", "mkt@agents/foo.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledRef != "mkt@agents/foo.md" {
		t.Errorf("expected removeFn called with 'mkt/agents/foo.md', got: %s", calledRef)
	}
	if !strings.Contains(out, "mkt@agents/foo.md") {
		t.Errorf("expected output to contain ref, got: %s", out)
	}
}

func TestRemoveCmd_Error(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		removeFn: func(ref domain.MctRef) (service.RemoveResult, error) {
			return service.RemoveResult{}, &domain.DomainError{Code: "ERR", Message: "remove failed"}
		},
	}
	_, err := runCmd(t, svc, "remove", "--ref", "mkt@agents/foo.md")
	if err == nil {
		t.Fatal("expected non-nil error, got nil")
	}
}

// ---------------------------------------------------------------------------
// prune command
// ---------------------------------------------------------------------------

func TestPruneCmd_AllKeep(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		pruneFn: func(opts service.PruneOpts) ([]service.PruneResult, error) {
			return []service.PruneResult{
				{Ref: "mkt@agents/gone.md", Action: "keep"},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "prune", "--all-keep")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "keep") {
		t.Errorf("expected output to contain 'keep', got: %s", out)
	}
}

func TestPruneCmd_AllRemove(t *testing.T) {
	var capturedOpts service.PruneOpts
	svc := mockServices()
	svc.Entries = &stubEntries{
		pruneFn: func(opts service.PruneOpts) ([]service.PruneResult, error) {
			capturedOpts = opts
			return nil, nil
		},
	}
	_, err := runCmd(t, svc, "prune", "--all-remove")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !capturedOpts.AllRemove {
		t.Errorf("expected AllRemove=true passed to pruneFn, got false")
	}
}

// ---------------------------------------------------------------------------
// init command
// ---------------------------------------------------------------------------

func TestInitCmd_Default(t *testing.T) {
	var called bool
	svc := mockServices()
	svc.Entries = &stubEntries{
		initFn: func(opts service.InitOpts) error {
			called = true
			return nil
		},
	}
	_, err := runCmd(t, svc, "init")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected initFn to be called")
	}
}

// ---------------------------------------------------------------------------
// index command
// ---------------------------------------------------------------------------

func TestIndexCmd_Default(t *testing.T) {
	svc := mockServices()
	svc.Search = &stubSearch{
		dumpIndexFn: func() ([]domain.Entry, error) {
			return []domain.Entry{
				{Ref: "mkt@agents/foo.md", Market: "mkt", Type: domain.EntryTypeAgent},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "index", "--dump")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "mkt") {
		t.Errorf("expected output to contain 'mkt', got: %s", out)
	}
}

func TestIndexCmd_JSON(t *testing.T) {
	svc := mockServices()
	svc.Search = &stubSearch{
		dumpIndexFn: func() ([]domain.Entry, error) {
			return []domain.Entry{
				{Ref: "mkt@agents/foo.md", Market: "mkt", Type: domain.EntryTypeAgent},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "index", "--dump")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
}

// ---------------------------------------------------------------------------
// import command
// ---------------------------------------------------------------------------

func TestImportCmd_BasicSuccess(t *testing.T) {
	// Write a valid export file to a temp location
	export := `{
  "version": 1,
  "markets": [],
  "entries": []
}`
	tmpFile, err := os.CreateTemp("", "mct-import-test-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	if _, err := tmpFile.WriteString(export); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{}, nil
		},
	}
	_, err = runCmd(t, svc, "import", tmpFile.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// config get command
// ---------------------------------------------------------------------------

func TestConfigGetCmd_Key(t *testing.T) {
	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{LocalPath: ".claude", ConflictPolicy: "block"}, nil
		},
	}
	out, err := runCmd(t, svc, "config", "get", "local_path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, ".claude") {
		t.Errorf("expected output to contain '.claude', got: %s", out)
	}
}

func TestConfigGetCmd_All(t *testing.T) {
	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{
				LocalPath:      ".claude",
				ConflictPolicy: "block",
				DriftPolicy:    "prompt",
			}, nil
		},
	}
	out, err := runCmd(t, svc, "config", "get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "local_path") {
		t.Errorf("expected output to contain 'local_path', got: %s", out)
	}
	if !strings.Contains(out, "conflict_policy") {
		t.Errorf("expected output to contain 'conflict_policy', got: %s", out)
	}
	if !strings.Contains(out, "drift_policy") {
		t.Errorf("expected output to contain 'drift_policy', got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// index command — additional branches
// ---------------------------------------------------------------------------

func TestIndexCmd_Bench(t *testing.T) {
	svc := mockServices()
	svc.Search = &stubSearch{
		benchIndexFn: func() (service.BenchResult, error) {
			return service.BenchResult{
				Entries: 42,
				Vocab:   100,
				Scan:    1 * time.Millisecond,
				Index:   2 * time.Millisecond,
				Total:   3 * time.Millisecond,
			}, nil
		},
	}
	out, err := runCmd(t, svc, "index", "--bench")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("expected output to contain '42', got: %s", out)
	}
	if !strings.Contains(out, "100") {
		t.Errorf("expected output to contain '100' (vocab), got: %s", out)
	}
}

func TestIndexCmd_NoFlag(t *testing.T) {
	svc := mockServices()
	_, err := runCmd(t, svc, "index")
	if err == nil {
		t.Fatal("expected error when neither --bench nor --dump given")
	}
}

// ---------------------------------------------------------------------------
// remove command — --all branches
// ---------------------------------------------------------------------------

func TestRemoveCmd_AllWithYes(t *testing.T) {
	var removed []domain.MctRef
	svc := mockServices()
	svc.Entries = &stubEntries{
		listFn: func(opts service.ListOpts) ([]domain.Entry, error) {
			return []domain.Entry{
				{Ref: "mkt@agents/a.md"},
				{Ref: "mkt@agents/b.md"},
			}, nil
		},
		removeFn: func(ref domain.MctRef) (service.RemoveResult, error) {
			removed = append(removed, ref)
			return service.RemoveResult{}, nil
		},
	}
	out, err := runCmd(t, svc, "remove", "--all", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 2 {
		t.Errorf("expected 2 removes, got %d", len(removed))
	}
	if !strings.Contains(out, "mkt@agents/a.md") {
		t.Errorf("expected output to contain 'mkt/agents/a.md', got: %s", out)
	}
}

func TestRemoveCmd_AllWithConfirmYes(t *testing.T) {
	var removed []domain.MctRef
	svc := mockServices()
	svc.Entries = &stubEntries{
		listFn: func(opts service.ListOpts) ([]domain.Entry, error) {
			return []domain.Entry{{Ref: "mkt@agents/c.md"}}, nil
		},
		removeFn: func(ref domain.MctRef) (service.RemoveResult, error) {
			removed = append(removed, ref)
			return service.RemoveResult{}, nil
		},
	}
	_, err := runCmdWithStdin(t, svc, "y\n", "remove", "--all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 1 {
		t.Errorf("expected 1 remove, got %d", len(removed))
	}
}

func TestRemoveCmd_AllWithConfirmNo(t *testing.T) {
	svc := mockServices()
	var removed []domain.MctRef
	svc.Entries = &stubEntries{
		listFn: func(opts service.ListOpts) ([]domain.Entry, error) {
			return []domain.Entry{{Ref: "mkt@agents/d.md"}}, nil
		},
		removeFn: func(ref domain.MctRef) (service.RemoveResult, error) {
			removed = append(removed, ref)
			return service.RemoveResult{}, nil
		},
	}
	out, err := runCmdWithStdin(t, svc, "n\n", "remove", "--all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected no removes, got %d", len(removed))
	}
	if !strings.Contains(out, "aborted") {
		t.Errorf("expected output to contain 'aborted', got: %s", out)
	}
}

func TestRemoveCmd_AllJSON(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		listFn: func(opts service.ListOpts) ([]domain.Entry, error) {
			return []domain.Entry{{Ref: "mkt@agents/e.md"}}, nil
		},
		removeFn: func(ref domain.MctRef) (service.RemoveResult, error) { return service.RemoveResult{}, nil },
	}
	out, err := runCmd(t, svc, "remove", "--all", "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
}

func TestRemoveCmd_AllError(t *testing.T) {
	svc := mockServices()
	svc.Entries = &stubEntries{
		listFn: func(opts service.ListOpts) ([]domain.Entry, error) {
			return []domain.Entry{{Ref: "mkt@agents/f.md"}}, nil
		},
		removeFn: func(ref domain.MctRef) (service.RemoveResult, error) {
			return service.RemoveResult{}, &domain.DomainError{Code: "ERR", Message: "disk error"}
		},
	}
	out, err := runCmd(t, svc, "remove", "--all", "--yes")
	if err != nil {
		t.Fatalf("remove --all should not propagate per-entry errors, got: %v", err)
	}
	if !strings.Contains(out, "disk error") {
		t.Errorf("expected error detail in output, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// import command — additional branches
// ---------------------------------------------------------------------------

func TestImportCmd_InvalidJSON(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "mct-import-bad-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.WriteString("not json at all")
	_ = tmpFile.Close()

	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) { return domain.Config{}, nil },
	}
	_, err = runCmd(t, svc, "import", tmpFile.Name())
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestImportCmd_DryRun(t *testing.T) {
	export := ExportData{
		Version: 1,
		Markets: []ExportMarket{{Name: "mkt", URL: "https://example.com", Branch: "main"}},
		Entries: []ExportEntry{},
	}
	data, _ := json.Marshal(export)
	tmpFile, err := os.CreateTemp("", "mct-import-dry-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.Write(data)
	_ = tmpFile.Close()

	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) { return domain.Config{}, nil },
	}
	out, err := runCmd(t, svc, "import", "--dry-run", tmpFile.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "(dry-run)") {
		t.Errorf("expected output to contain '(dry-run)', got: %s", out)
	}
}

func TestImportCmd_SkipExistingMarket(t *testing.T) {
	export := ExportData{
		Version: 1,
		Markets: []ExportMarket{{Name: "mkt", URL: "https://example.com/repo.git", Branch: "main"}},
		Entries: []ExportEntry{},
	}
	data, _ := json.Marshal(export)
	tmpFile, err := os.CreateTemp("", "mct-import-skip-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.Write(data)
	_ = tmpFile.Close()

	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) {
			return domain.Config{
				Markets: []domain.MarketConfig{
					{Name: "mkt", URL: "https://example.com/repo.git"},
				},
			}, nil
		},
	}
	out, err := runCmd(t, svc, "import", tmpFile.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "already registered") {
		t.Errorf("expected output to mention 'already registered' for duplicate market URL, got: %s", out)
	}
}

func TestImportCmd_JSONOutput(t *testing.T) {
	export := ExportData{
		Version: 1,
		Markets: []ExportMarket{{Name: "mkt", URL: "https://example.com/new.git", Branch: "main"}},
		Entries: []ExportEntry{},
	}
	data, _ := json.Marshal(export)
	tmpFile, err := os.CreateTemp("", "mct-import-json-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_, _ = tmpFile.Write(data)
	_ = tmpFile.Close()

	var addCalled bool
	svc := mockServices()
	svc.Config = &stubConfig{
		getConfigFn: func() (domain.Config, error) { return domain.Config{}, nil },
	}
	svc.Markets = &stubMarkets{
		addMarketFn: func(url string, opts service.AddMarketOpts) (service.AddMarketResult, error) {
			addCalled = true
			return service.AddMarketResult{}, nil
		},
	}
	out, err := runCmd(t, svc, "import", "--yes", "--json", tmpFile.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustValidJSON(t, out)
	if !addCalled {
		t.Error("expected AddMarket to be called for new market")
	}
}
