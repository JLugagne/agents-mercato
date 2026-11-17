package gitadapter

import (
	"bufio"
	"bytes"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func isHTTPURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "http://")
}

func isHTTPAuthError(err error) bool {
	return errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed)
}

func withHTTPCredentials(rawURL string, op func(transport.AuthMethod) error) error {
	err := op(resolveAuth(rawURL))
	if err == nil || !isHTTPURL(rawURL) || !isHTTPAuthError(err) {
		return err
	}
	desc, ok := credentialDescription(rawURL)
	if !ok {
		return err
	}
	auth, ok := gitCredentialFill(desc)
	if !ok {
		return err
	}
	err = op(auth)
	switch {
	case err == nil:
		gitCredentialRun("approve", desc, auth)
	case isHTTPAuthError(err):
		gitCredentialRun("reject", desc, auth)
	}
	return err
}

func credentialDescription(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString("protocol=" + u.Scheme + "\n")
	b.WriteString("host=" + u.Host + "\n")
	if p := strings.TrimPrefix(u.Path, "/"); p != "" {
		b.WriteString("path=" + p + "\n")
	}
	if u.User != nil && u.User.Username() != "" {
		b.WriteString("username=" + u.User.Username() + "\n")
	}
	return b.String(), true
}

func gitCredentialCmd(action, input string) *exec.Cmd {
	cmd := exec.Command("git", "credential", action)
	cmd.Stdin = strings.NewReader(input + "\n")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd
}

func gitCredentialFill(desc string) (*githttp.BasicAuth, bool) {
	out, err := gitCredentialCmd("fill", desc).Output()
	if err != nil {
		return nil, false
	}
	auth := &githttp.BasicAuth{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, value, found := strings.Cut(sc.Text(), "=")
		if !found {
			continue
		}
		switch key {
		case "username":
			auth.Username = value
		case "password":
			auth.Password = value
		}
	}
	if auth.Password == "" {
		return nil, false
	}
	return auth, true
}

func gitCredentialRun(action, desc string, auth *githttp.BasicAuth) {
	if !strings.Contains(desc, "username=") {
		desc += "username=" + auth.Username + "\n"
	}
	_ = gitCredentialCmd(action, desc+"password="+auth.Password+"\n").Run()
}
