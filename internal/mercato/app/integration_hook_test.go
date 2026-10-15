package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
)

func hookMarketFiles() map[string]string {
	return map[string]string{
		"dev/go/hooks/go-vet.json": `{
  "event": "PreToolUse",
  "matcher": "Bash",
  "hooks": [
    { "type": "command", "command": "go vet ./..." }
  ]
}`,
		"dev/go/README.md": "---\ntags:\n  - go\n---\nGo profile.\n",
	}
}

func TestIntegration_AddHook(t *testing.T) {
	app, projectDir, _ := setupIntegration(t, hookMarketFiles())

	ref := domain.MctRef(marketName + "@dev/go/hooks/go-vet.json")
	if _, err := app.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	settingsPath := filepath.Join(projectDir, ".claude", "settings.json")
	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	settings := string(got)
	if !strings.Contains(settings, `"PreToolUse"`) {
		t.Errorf("expected PreToolUse event in settings.json:\n%s", settings)
	}
	if !strings.Contains(settings, "go vet") {
		t.Errorf("expected hook command in settings.json:\n%s", settings)
	}
	if strings.Contains(settings, "mct_id") {
		t.Errorf("settings.json must not carry mct_id (schema rejects unknown keys):\n%s", settings)
	}
	owners, err := os.ReadFile(filepath.Join(projectDir, ".claude", "mct-hooks.json"))
	if err != nil {
		t.Fatalf("read mct-hooks.json: %v", err)
	}
	if !strings.Contains(string(owners), mctIDForRef(ref)) {
		t.Errorf("expected ownership recorded in mct-hooks.json:\n%s", owners)
	}

	db, err := cfgadapter.NewInstallDB().Load(app.cacheDir)
	if err != nil {
		t.Fatalf("load installdb: %v", err)
	}
	im := db.FindMarket(marketName)
	if im == nil {
		t.Fatal("market not in installdb")
	}
	found := false
	for _, pkg := range im.Packages {
		for _, h := range pkg.Files.Hooks {
			if h == "go-vet.json" {
				found = true
			}
		}
	}
	if !found {
		t.Error("hook not recorded in installdb")
	}
}

func TestIntegration_HookCheckCleanThenDriftThenClean(t *testing.T) {
	app, projectDir, _ := setupIntegration(t, hookMarketFiles())
	ref := domain.MctRef(marketName + "@dev/go/hooks/go-vet.json")
	if _, err := app.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Clean immediately after add.
	statuses, err := app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	hookStatus := findStatus(statuses, ref)
	if hookStatus == nil {
		t.Fatalf("hook ref %q not in Check results: %+v", ref, statuses)
	}
	if hookStatus.State != domain.StateClean {
		t.Errorf("expected StateClean post-add, got %v", hookStatus.State)
	}

	// External edit: rewrite the hook command in settings.json.
	settingsPath := filepath.Join(projectDir, ".claude", "settings.json")
	src, _ := os.ReadFile(settingsPath)
	tampered := strings.Replace(string(src), "go vet ./...", "rm -rf /", 1)
	if err := os.WriteFile(settingsPath, []byte(tampered), 0644); err != nil {
		t.Fatal(err)
	}

	statuses, err = app.Check(service.CheckOpts{})
	if err != nil {
		t.Fatalf("Check after tamper: %v", err)
	}
	hookStatus = findStatus(statuses, ref)
	if hookStatus == nil {
		t.Fatal("hook missing after tamper")
	}
	if hookStatus.State != domain.StateDrift {
		t.Errorf("expected StateDrift after tamper, got %v", hookStatus.State)
	}
}

func TestIntegration_HookRemovePreservesSiblingKeys(t *testing.T) {
	app, projectDir, _ := setupIntegration(t, hookMarketFiles())

	// Pre-seed settings.json with an unrelated user-owned key that mct
	// must not touch on install or remove.
	settingsPath := filepath.Join(projectDir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"theme":"dark","max_tokens":4096}`), 0644); err != nil {
		t.Fatal(err)
	}

	ref := domain.MctRef(marketName + "@dev/go/hooks/go-vet.json")
	if _, err := app.Add(ref, service.AddOpts{}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := app.Remove(ref, service.RemoveOpts{}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	gotStr := string(got)
	if !strings.Contains(gotStr, `"theme"`) || !strings.Contains(gotStr, "dark") {
		t.Errorf("user theme key was lost during install/remove cycle:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "max_tokens") || !strings.Contains(gotStr, "4096") {
		t.Errorf("user max_tokens key was lost:\n%s", gotStr)
	}
	if strings.Contains(gotStr, `"hooks"`) {
		t.Errorf("hooks key should be dropped when no hooks remain:\n%s", gotStr)
	}
	if strings.Contains(gotStr, "go vet") {
		t.Errorf("hook command was not removed:\n%s", gotStr)
	}

	// Removed hook must no longer be listed.
	all, err := app.List(service.ListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		if e.Type == domain.EntryTypeHook {
			t.Errorf("List should not return removed hook: %v", e.Ref)
		}
	}
}

func findStatus(statuses []domain.EntryStatus, ref domain.MctRef) *domain.EntryStatus {
	for i := range statuses {
		if statuses[i].Ref == ref {
			return &statuses[i]
		}
	}
	return nil
}
