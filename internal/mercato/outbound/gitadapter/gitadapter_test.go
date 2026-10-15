package gitadapter_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/gitadapter"
)

func commitFiles(t *testing.T, repo *git.Repository, dir string, files map[string]string, msg string) string {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	for p, c := range files {
		abs := filepath.Join(dir, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(c), 0o644))
		_, err := wt.Add(p)
		require.NoError(t, err)
	}
	h, err := wt.Commit(msg, &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}})
	require.NoError(t, err)
	return h.String()
}

func newUpstream(t *testing.T) (string, *git.Repository) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	return dir, repo
}

func TestFileVersion_ShallowClone(t *testing.T) {
	upDir, up := newUpstream(t)
	commitFiles(t, up, upDir, map[string]string{"dev/go/agents/a.md": "a1"}, "c1")
	commitFiles(t, up, upDir, map[string]string{"dev/go/agents/b.md": "b1"}, "c2")
	head := commitFiles(t, up, upDir, map[string]string{"dev/go/agents/c.md": "c1"}, "c3")

	clone := filepath.Join(t.TempDir(), "clone")
	a := gitadapter.New()
	require.NoError(t, a.Clone("file://"+upDir, clone))

	v, err := a.FileVersion(clone, "dev/go/agents/a.md")
	require.NoError(t, err)
	require.NotEmpty(t, v)

	v, err = a.FileVersion(clone, "dev/go/agents/c.md")
	require.NoError(t, err)
	require.Contains(t, string(v), head[:7])

	_, err = a.FileVersion(clone, "dev/go/agents/missing.md")
	require.Error(t, err)
}

func TestReadMarketFiles_ShallowCloneVersions(t *testing.T) {
	upDir, up := newUpstream(t)
	commitFiles(t, up, upDir, map[string]string{"dev/go/agents/a.md": "a1"}, "c1")
	head := commitFiles(t, up, upDir, map[string]string{"dev/go/agents/b.md": "b1"}, "c2")

	clone := filepath.Join(t.TempDir(), "clone")
	a := gitadapter.New()
	require.NoError(t, a.Clone("file://"+upDir, clone))

	files, err := a.ReadMarketFiles(clone, "master")
	require.NoError(t, err)
	require.Len(t, files, 2)
	for _, f := range files {
		require.NotEmpty(t, f.Version, f.Path)
		require.Contains(t, string(f.Version), head[:7], f.Path)
	}
}

func TestReadMarketFiles_FullHistoryVersions(t *testing.T) {
	upDir, up := newUpstream(t)
	first := commitFiles(t, up, upDir, map[string]string{"dev/go/agents/a.md": "a1"}, "c1")
	second := commitFiles(t, up, upDir, map[string]string{"dev/go/agents/b.md": "b1"}, "c2")

	clone := filepath.Join(t.TempDir(), "clone")
	a := gitadapter.New(gitadapter.WithDepth(0))
	require.NoError(t, a.Clone("file://"+upDir, clone))

	files, err := a.ReadMarketFiles(clone, "master")
	require.NoError(t, err)
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = string(f.Version)
	}
	require.Contains(t, got["dev/go/agents/a.md"], first[:7])
	require.Contains(t, got["dev/go/agents/b.md"], second[:7])
}
