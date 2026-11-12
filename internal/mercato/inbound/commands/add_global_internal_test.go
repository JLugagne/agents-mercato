package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func TestAdd_GlobalRunsFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	var cwd string
	svc := mockServices()
	svc.Entries = &stubEntries{
		addFn: func(ref domain.MctRef, opts service.AddOpts) (service.AddResult, error) {
			cwd, _ = os.Getwd()
			return service.AddResult{}, nil
		},
	}
	if _, err := runCmd(t, svc, "add", "mkt@agents/foo.md", "--global"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, _ := filepath.EvalSymlinks(home)
	got, _ := filepath.EvalSymlinks(cwd)
	if got != want {
		t.Errorf("expected add to run from %s, ran from %s", want, got)
	}
}
