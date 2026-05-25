package gitadapter

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	sshconfig "github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

func isAgentOrSkillPath(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		if p == "agents" || p == "skills" || p == "commands" || p == "hooks" {
			return true
		}
	}
	return false
}

func isReadmePath(path string) bool {
	return strings.EqualFold(filepath.Base(path), "README.md")
}

// resolveAuth returns SSH auth for SSH URLs, nil for HTTPS.
// It tries the SSH agent first; if the agent has no keys, it falls back
// to common key files on disk (~/.ssh/id_ed25519, id_rsa, etc.).
func resolveAuth(url string) transport.AuthMethod {
	if !isSSHURL(url) {
		return nil
	}

	hostKeyCB := hostKeyCallback()

	// Try SSH agent first (with timeout to avoid hanging on unresponsive agents).
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.DialTimeout("unix", sock, 5*time.Second); err == nil {
			ag := agent.NewClient(conn)
			if keys, err := ag.List(); err == nil && len(keys) > 0 {
				_ = conn.Close()
				auth, err := gitssh.NewSSHAgentAuth("git")
				if err == nil {
					auth.HostKeyCallback = hostKeyCB
					return auth
				}
			}
			_ = conn.Close()
		}
	}

	// Fall back to key files on disk.
	// First, resolve IdentityFile from ~/.ssh/config for the target host.
	// Then try well-known key names.
	home := os.Getenv("HOME")
	var candidates []string
	if host := extractHost(url); host != "" {
		if identity := sshConfigGet(home, host, "IdentityFile"); identity != "" {
			candidates = append(candidates, expandHome(identity, home))
		}
	}
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		candidates = append(candidates, filepath.Join(home, ".ssh", name))
	}
	for _, keyPath := range candidates {
		if _, err := os.Stat(keyPath); err != nil {
			continue
		}
		auth, err := gitssh.NewPublicKeysFromFile("git", keyPath, "")
		if err != nil {
			if !isPassphraseError(err) {
				continue
			}
			passphrase, pErr := promptPassphrase(keyPath)
			if pErr != nil {
				continue
			}
			auth, err = gitssh.NewPublicKeysFromFile("git", keyPath, string(passphrase))
			if err != nil {
				continue
			}
		}
		auth.HostKeyCallback = hostKeyCB
		return auth
	}

	return nil
}

// extractHost returns the hostname from an SSH git URL.
func extractHost(rawURL string) string {
	// git@github.com:user/repo.git
	if strings.HasPrefix(rawURL, "git@") || (strings.Contains(rawURL, "@") && strings.Contains(rawURL, ":")) {
		at := strings.Index(rawURL, "@")
		rest := rawURL[at+1:]
		if colon := strings.Index(rest, ":"); colon > 0 {
			return rest[:colon]
		}
	}
	// ssh://git@github.com/user/repo.git
	if u, err := url.Parse(rawURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return ""
}

// expandHome replaces a leading ~ with the home directory.
func expandHome(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// sshConfigGet reads ~/.ssh/config and returns the value for the given host and key.
func sshConfigGet(home, host, key string) string {
	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	cfg, err := sshconfig.Decode(f)
	if err != nil {
		return ""
	}
	val, err := cfg.Get(host, key)
	if err != nil {
		return ""
	}
	return val
}

func hostKeyCallback() ssh.HostKeyCallback {
	knownHostsPath := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
	cb, err := gitssh.NewKnownHostsCallback(knownHostsPath)
	if err == nil {
		return cb
	}
	// If known_hosts is missing or unreadable, reject all host keys rather
	// than silently accepting them (which would allow MITM attacks).
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return fmt.Errorf("host key verification failed for %s: no known_hosts file found at %s", hostname, knownHostsPath)
	}
}

// resolveAuthFromRepo reads the origin remote URL from an opened repo and returns auth.
func resolveAuthFromRepo(repo *git.Repository) transport.AuthMethod {
	remote, err := repo.Remote("origin")
	if err != nil || remote == nil {
		return nil
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return nil
	}
	return resolveAuth(urls[0])
}

// isPassphraseError returns true when the error indicates the key is
// encrypted and requires a passphrase to decrypt.
func isPassphraseError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "x509:") ||
		strings.Contains(msg, "bcrypt_pbkdf") ||
		strings.Contains(msg, "encrypted") ||
		strings.Contains(msg, "passphrase") ||
		strings.Contains(msg, "this private key is passphrase protected")
}

// promptPassphrase asks the user for an SSH key passphrase via the terminal.
func promptPassphrase(keyPath string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("not a terminal, cannot prompt for passphrase")
	}
	fmt.Fprintf(os.Stderr, "Enter passphrase for %s: ", keyPath)
	passphrase, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr) // newline after hidden input
	if err != nil {
		return nil, err
	}
	return passphrase, nil
}

func isSSHURL(url string) bool {
	return strings.HasPrefix(url, "git@") ||
		strings.HasPrefix(url, "ssh://") ||
		(strings.Contains(url, "@") && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://"))
}

// isHookPath returns true when path is a hook snippet — a .json file under
// a hooks/ segment. Used by the market scanner to include hook entries
// alongside agents/skills.
func isHookPath(path string) bool {
	if !strings.HasSuffix(path, ".json") {
		return false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		if p == "hooks" {
			return true
		}
	}
	return false
}
