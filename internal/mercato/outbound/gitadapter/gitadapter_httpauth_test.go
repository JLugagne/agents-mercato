package gitadapter_test

import (
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/gitadapter"
)

const (
	httpUser = "alice"
	httpPass = "s3cret"
)

func serveAuthenticatedRepo(t *testing.T) (string, string) {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary required for git http-backend")
	}

	upDir, up := newUpstream(t)
	commitFiles(t, up, upDir, map[string]string{"dev/go/agents/a.md": "a1"}, "c1")

	root := t.TempDir()
	_, err = git.PlainClone(filepath.Join(root, "market.git"), true, &git.CloneOptions{URL: "file://" + upDir})
	require.NoError(t, err)

	backend := &cgi.Handler{
		Path: gitBin,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != httpUser || p != httpPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "HTTP Basic: Access denied", http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/market.git", upDir
}

func useCredentialHelper(t *testing.T, user, pass string) {
	t.Helper()
	home := t.TempDir()
	cfg := filepath.Join(home, "gitconfig")
	helper := "!f() { test \"$1\" = get && printf 'username=" + user + "\\npassword=" + pass + "\\n'; }; f"
	require.NoError(t, exec.Command("git", "config", "--file", cfg, "credential.helper", helper).Run())
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

func noCredentialHelper(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

func TestValidateRemote_HTTPUsesGitCredentialHelper(t *testing.T) {
	url, _ := serveAuthenticatedRepo(t)
	useCredentialHelper(t, httpUser, httpPass)

	require.NoError(t, gitadapter.New().ValidateRemote(url))
}

func TestClone_HTTPUsesGitCredentialHelper(t *testing.T) {
	url, _ := serveAuthenticatedRepo(t)
	useCredentialHelper(t, httpUser, httpPass)

	clone := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, gitadapter.New().Clone(url, clone))
	_, err := os.Stat(filepath.Join(clone, "dev/go/agents/a.md"))
	require.NoError(t, err)
}

func TestFetch_HTTPUsesGitCredentialHelper(t *testing.T) {
	url, upDir := serveAuthenticatedRepo(t)
	useCredentialHelper(t, httpUser, httpPass)

	clone := filepath.Join(t.TempDir(), "clone")
	repo, err := git.PlainClone(clone, false, &git.CloneOptions{URL: "file://" + upDir})
	require.NoError(t, err)
	require.NoError(t, repo.DeleteRemote("origin"))
	_, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{url}})
	require.NoError(t, err)

	branch, err := gitadapter.New().DefaultBranch(clone)
	require.NoError(t, err)
	_, err = gitadapter.New().Fetch(clone, branch)
	require.NoError(t, err)
}

func TestClone_HTTPWithoutCredentialsFails(t *testing.T) {
	url, _ := serveAuthenticatedRepo(t)
	noCredentialHelper(t)

	clone := filepath.Join(t.TempDir(), "clone")
	require.Error(t, gitadapter.New().Clone(url, clone))
	_, err := os.Stat(clone)
	require.True(t, os.IsNotExist(err))
}

func TestClone_HTTPWrongCredentialsFails(t *testing.T) {
	url, _ := serveAuthenticatedRepo(t)
	useCredentialHelper(t, httpUser, "wrong")

	clone := filepath.Join(t.TempDir(), "clone")
	require.Error(t, gitadapter.New().Clone(url, clone))
}
