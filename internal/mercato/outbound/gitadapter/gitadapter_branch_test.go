package gitadapter_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/gitadapter"
)

func TestDefaultBranch_MasterAfterFetch(t *testing.T) {
	upDir, up := newUpstream(t)
	commitFiles(t, up, upDir, map[string]string{"dev/go/agents/a.md": "a1"}, "c1")

	clone := filepath.Join(t.TempDir(), "clone")
	a := gitadapter.New()
	require.NoError(t, a.Clone("file://"+upDir, clone))

	b, err := a.DefaultBranch(clone)
	require.NoError(t, err)
	require.Equal(t, "master", b)

	commitFiles(t, up, upDir, map[string]string{"dev/go/agents/b.md": "b1"}, "c2")
	_, err = a.Fetch(clone, "master")
	require.NoError(t, err)

	b, err = a.DefaultBranch(clone)
	require.NoError(t, err)
	require.Equal(t, "master", b)
}

func TestClone_DoesNotWriteToStdout(t *testing.T) {
	upDir, up := newUpstream(t)
	for i := range 30 {
		commitFiles(t, up, upDir, map[string]string{filepath.Join("dev/go/agents", string(rune('a'+i%26))+".md"): string(rune(i))}, "c")
	}

	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()

	clone := filepath.Join(t.TempDir(), "clone")
	cloneErr := gitadapter.New().Clone("file://"+upDir, clone)

	os.Stdout = orig
	require.NoError(t, w.Close())
	out := <-done
	require.NoError(t, cloneErr)
	require.Empty(t, string(out))
}
