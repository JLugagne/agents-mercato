package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/stretchr/testify/require"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

type fakeGitHub struct {
	tag       string
	archive   []byte
	checksums string
	signature []byte
	downloads int
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+repoOwner+"/"+repoName+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		assets := []map[string]string{
			{"name": "mct_linux_amd64.tar.gz", "browser_download_url": srv.URL + "/dl/archive"},
			{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums"},
		}
		if f.signature != nil {
			assets = append(assets, map[string]string{"name": "checksums.txt.sig", "browser_download_url": srv.URL + "/dl/signature"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "assets": assets})
	})
	mux.HandleFunc("/dl/archive", func(w http.ResponseWriter, r *http.Request) {
		f.downloads++
		_, _ = w.Write(f.archive)
	})
	mux.HandleFunc("/dl/checksums", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(f.checksums))
	})
	mux.HandleFunc("/dl/signature", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(f.signature)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newFake(t *testing.T, tag, binary string) *fakeGitHub {
	t.Helper()
	archive := tarGz(t, map[string]string{"LICENSE": "MIT", "README.md": "readme", "mct": binary})
	sum := sha256.Sum256(archive)
	return &fakeGitHub{
		tag:       tag,
		archive:   archive,
		checksums: hex.EncodeToString(sum[:]) + "  mct_linux_amd64.tar.gz\n" + "deadbeef  mct_darwin_arm64.tar.gz\n",
	}
}

func testUpgrader(t *testing.T, srv *httptest.Server) (upgrader, string) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "mct")
	require.NoError(t, os.WriteFile(exe, []byte("old binary"), 0o755))
	return upgrader{client: srv.Client(), apiBase: srv.URL, goos: "linux", goarch: "amd64", exe: exe}, exe
}

func TestUpgrader_ReplacesBinaryWithVerifiedRelease(t *testing.T) {
	fake := newFake(t, "v1.9.0", "new binary")
	u, exe := testUpgrader(t, fake.server(t))

	latest, updated, err := u.upgrade(context.Background(), "v1.8.7")

	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, "v1.9.0", latest)
	got, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "new binary", string(got))
	info, err := os.Stat(exe)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestUpgrader_ChecksumMismatchKeepsBinary(t *testing.T) {
	fake := newFake(t, "v1.9.0", "new binary")
	fake.checksums = "0000000000000000000000000000000000000000000000000000000000000000  mct_linux_amd64.tar.gz\n"
	u, exe := testUpgrader(t, fake.server(t))

	_, _, err := u.upgrade(context.Background(), "v1.8.7")

	require.ErrorIs(t, err, errChecksumMismatch)
	got, _ := os.ReadFile(exe)
	require.Equal(t, "old binary", string(got))
}

func TestUpgrader_MissingChecksumEntryIsAnError(t *testing.T) {
	fake := newFake(t, "v1.9.0", "new binary")
	fake.checksums = "deadbeef  mct_darwin_arm64.tar.gz\n"
	u, exe := testUpgrader(t, fake.server(t))

	_, _, err := u.upgrade(context.Background(), "v1.8.7")

	require.ErrorIs(t, err, errNoChecksum)
	got, _ := os.ReadFile(exe)
	require.Equal(t, "old binary", string(got))
}

func TestUpgrader_UnsupportedPlatformIsAnError(t *testing.T) {
	fake := newFake(t, "v1.9.0", "new binary")
	u, _ := testUpgrader(t, fake.server(t))
	u.goos = "windows"

	_, _, err := u.upgrade(context.Background(), "v1.8.7")

	require.ErrorIs(t, err, errNoAsset)
}

func TestUpgrader_AlreadyLatestOrNewerDownloadsNothing(t *testing.T) {
	fake := newFake(t, "v1.9.0", "new binary")
	u, exe := testUpgrader(t, fake.server(t))

	for _, current := range []string{"v1.9.0", "v1.10.0"} {
		latest, updated, err := u.upgrade(context.Background(), current)
		require.NoError(t, err)
		require.False(t, updated, current)
		require.Equal(t, "v1.9.0", latest)
	}
	require.Zero(t, fake.downloads)
	got, _ := os.ReadFile(exe)
	require.Equal(t, "old binary", string(got))
}

func TestUpgrader_LatestTag(t *testing.T) {
	fake := newFake(t, "v1.9.0", "new binary")
	u, _ := testUpgrader(t, fake.server(t))

	tag, err := u.latestTag(context.Background())

	require.NoError(t, err)
	require.Equal(t, "v1.9.0", tag)
}

func TestNewerVersion(t *testing.T) {
	require.True(t, newerVersion("v1.9.0", "v1.8.7"))
	require.True(t, newerVersion("v1.10.0", "v1.9.9"))
	require.False(t, newerVersion("v1.9.0", "v1.9.0"))
	require.False(t, newerVersion("v1.8.0", "v1.9.0"))
	require.True(t, newerVersion("v1.9.0", "dev"))
}

func signingEntity(t *testing.T) (*openpgp.Entity, []byte) {
	t.Helper()
	e, err := openpgp.NewEntity("mct release", "", "release@example.com", nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, e.Serialize(w))
	require.NoError(t, w.Close())
	return e, buf.Bytes()
}

func detachSign(t *testing.T, e *openpgp.Entity, data string, armored bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	if armored {
		require.NoError(t, openpgp.ArmoredDetachSign(&buf, e, bytes.NewReader([]byte(data)), nil))
	} else {
		require.NoError(t, openpgp.DetachSign(&buf, e, bytes.NewReader([]byte(data)), nil))
	}
	return buf.Bytes()
}

func TestUpgrader_SignedChecksumsAccepted(t *testing.T) {
	for _, armored := range []bool{false, true} {
		e, pub := signingEntity(t)
		fake := newFake(t, "v1.9.0", "new binary")
		fake.signature = detachSign(t, e, fake.checksums, armored)
		u, exe := testUpgrader(t, fake.server(t))
		u.signingKey = pub

		_, updated, err := u.upgrade(context.Background(), "v1.8.7")

		require.NoError(t, err, "armored=%v", armored)
		require.True(t, updated)
		got, _ := os.ReadFile(exe)
		require.Equal(t, "new binary", string(got))
	}
}

func TestUpgrader_UnsignedReleaseRejectedWhenKeyEmbedded(t *testing.T) {
	_, pub := signingEntity(t)
	fake := newFake(t, "v1.9.0", "new binary")
	u, exe := testUpgrader(t, fake.server(t))
	u.signingKey = pub

	_, _, err := u.upgrade(context.Background(), "v1.8.7")

	require.ErrorIs(t, err, errUnsigned)
	require.Zero(t, fake.downloads)
	got, _ := os.ReadFile(exe)
	require.Equal(t, "old binary", string(got))
}

func TestUpgrader_SignatureFromOtherKeyRejected(t *testing.T) {
	_, pub := signingEntity(t)
	other, _ := signingEntity(t)
	fake := newFake(t, "v1.9.0", "new binary")
	fake.signature = detachSign(t, other, fake.checksums, false)
	u, exe := testUpgrader(t, fake.server(t))
	u.signingKey = pub

	_, _, err := u.upgrade(context.Background(), "v1.8.7")

	require.ErrorIs(t, err, errBadSignature)
	got, _ := os.ReadFile(exe)
	require.Equal(t, "old binary", string(got))
}

func TestUpgrader_TamperedChecksumsRejected(t *testing.T) {
	e, pub := signingEntity(t)
	fake := newFake(t, "v1.9.0", "evil binary")
	fake.signature = detachSign(t, e, "0000  mct_linux_amd64.tar.gz\n", false)
	u, exe := testUpgrader(t, fake.server(t))
	u.signingKey = pub

	_, _, err := u.upgrade(context.Background(), "v1.8.7")

	require.ErrorIs(t, err, errBadSignature)
	got, _ := os.ReadFile(exe)
	require.Equal(t, "old binary", string(got))
}

func TestUpgrader_InvalidEmbeddedKeyIsAnError(t *testing.T) {
	fake := newFake(t, "v1.9.0", "new binary")
	u, _ := testUpgrader(t, fake.server(t))
	u.signingKey = []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\ngarbage\n-----END PGP PUBLIC KEY BLOCK-----\n")

	_, _, err := u.upgrade(context.Background(), "v1.8.7")

	require.Error(t, err)
}
