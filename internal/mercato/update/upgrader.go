package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
)

const (
	githubAPI       = "https://api.github.com"
	checksumsAsset  = "checksums.txt"
	signatureAsset  = "checksums.txt.sig"
	binaryName      = "mct"
	maxArchiveBytes = 200 << 20
	maxSmallBytes   = 1 << 20
)

var (
	errNoAsset          = errors.New("no release asset for this platform")
	errNoChecksum       = errors.New("archive is not listed in checksums.txt")
	errChecksumMismatch = errors.New("archive checksum mismatch")
	errNoBinary         = errors.New("archive does not contain the mct binary")
	errUnsigned         = errors.New("release is not signed (checksums.txt.sig missing)")
	errBadSignature     = errors.New("checksums.txt signature verification failed")
)

//go:embed signing_key.asc
var signingKey []byte

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type upgrader struct {
	client     *http.Client
	apiBase    string
	goos       string
	goarch     string
	exe        string
	signingKey []byte
}

func defaultUpgrader() (upgrader, error) {
	exe, err := os.Executable()
	if err != nil {
		return upgrader{}, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return upgrader{
		client:     &http.Client{Timeout: 5 * time.Minute},
		apiBase:    githubAPI,
		goos:       runtime.GOOS,
		goarch:     runtime.GOARCH,
		exe:        exe,
		signingKey: signingKey,
	}, nil
}

func (u upgrader) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(url, u.apiBase) {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("GET %s: response larger than %d bytes", url, limit)
	}
	return data, nil
}

func (u upgrader) latestRelease(ctx context.Context) (release, error) {
	data, err := u.get(ctx, u.apiBase+"/repos/"+repoOwner+"/"+repoName+"/releases/latest", maxSmallBytes)
	if err != nil {
		return release{}, err
	}
	var r release
	if err := json.Unmarshal(data, &r); err != nil {
		return release{}, err
	}
	if r.TagName == "" {
		return release{}, errors.New("latest release has no tag")
	}
	return r, nil
}

func (u upgrader) latestTag(ctx context.Context) (string, error) {
	r, err := u.latestRelease(ctx)
	return r.TagName, err
}

func (u upgrader) upgrade(ctx context.Context, current string) (string, bool, error) {
	rel, err := u.latestRelease(ctx)
	if err != nil {
		return "", false, err
	}
	if !newerVersion(rel.TagName, current) {
		return rel.TagName, false, nil
	}

	archiveName := fmt.Sprintf("%s_%s_%s.tar.gz", binaryName, u.goos, u.goarch)
	archiveURL, sumsURL, sigURL := "", "", ""
	for _, a := range rel.Assets {
		switch a.Name {
		case archiveName:
			archiveURL = a.URL
		case checksumsAsset:
			sumsURL = a.URL
		case signatureAsset:
			sigURL = a.URL
		}
	}
	if archiveURL == "" || sumsURL == "" {
		return "", false, errors.Join(errNoAsset, fmt.Errorf("%s in %s", archiveName, rel.TagName))
	}

	sums, err := u.get(ctx, sumsURL, maxSmallBytes)
	if err != nil {
		return "", false, err
	}
	if len(bytes.TrimSpace(u.signingKey)) > 0 {
		if sigURL == "" {
			return "", false, errUnsigned
		}
		sig, err := u.get(ctx, sigURL, maxSmallBytes)
		if err != nil {
			return "", false, err
		}
		if err := verifySignature(u.signingKey, sums, sig); err != nil {
			return "", false, err
		}
	}
	want, ok := checksumFor(sums, archiveName)
	if !ok {
		return "", false, errNoChecksum
	}
	archive, err := u.get(ctx, archiveURL, maxArchiveBytes)
	if err != nil {
		return "", false, err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return "", false, errChecksumMismatch
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return "", false, err
	}
	if err := replaceFile(u.exe, binary); err != nil {
		return "", false, err
	}
	return rel.TagName, true, nil
}

func verifySignature(armoredKey, data, sig []byte) error {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armoredKey))
	if err != nil {
		return errors.Join(errors.New("invalid embedded signing key"), err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(sig), []byte("-----BEGIN")) {
		_, err = openpgp.CheckArmoredDetachedSignature(keyring, bytes.NewReader(data), bytes.NewReader(sig), nil)
	} else {
		_, err = openpgp.CheckDetachedSignature(keyring, bytes.NewReader(data), bytes.NewReader(sig), nil)
	}
	if err != nil {
		return errors.Join(errBadSignature, err)
	}
	return nil
}

func checksumFor(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errNoBinary
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != binaryName {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxArchiveBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxArchiveBytes {
			return nil, errors.New("mct binary in archive is too large")
		}
		return data, nil
	}
}

func replaceFile(target string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".mct-upgrade-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if j := strings.IndexAny(p, "-+"); j >= 0 {
			p = p[:j]
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func newerVersion(latest, current string) bool {
	l, ok := parseVersion(latest)
	if !ok {
		return false
	}
	c, ok := parseVersion(current)
	if !ok {
		return true
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}
