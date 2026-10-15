package txadapter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/txadapter"
)

func TestCommit_DeleteAllThenWriteKeepsWrite(t *testing.T) {
	dest := t.TempDir()
	dir := filepath.Join(dest, "skills", "foo")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old.md"), []byte("old"), 0o644))

	mgr := txadapter.New(filepath.Join(t.TempDir(), "staging"))
	tx, err := mgr.Begin("test")
	require.NoError(t, err)
	require.NoError(t, tx.DeleteAll(dir))
	require.NoError(t, tx.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("new")))
	require.NoError(t, tx.Commit())

	got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "new", string(got))
	_, err = os.Stat(filepath.Join(dir, "old.md"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCommit_WriteThenDeleteRemovesFile(t *testing.T) {
	dest := t.TempDir()
	target := filepath.Join(dest, "a.md")

	mgr := txadapter.New(filepath.Join(t.TempDir(), "staging"))
	tx, err := mgr.Begin("test")
	require.NoError(t, err)
	require.NoError(t, tx.WriteFile(target, []byte("x")))
	require.NoError(t, tx.DeleteFile(target))
	require.NoError(t, tx.Commit())

	_, err = os.Stat(target)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRecoverPending_DoesNotReplayFailedInProcessCommit(t *testing.T) {
	dest := t.TempDir()
	blocker := filepath.Join(dest, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("file, not dir"), 0o644))
	db := filepath.Join(dest, "installed.json")
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	mgr := txadapter.New(stagingRoot)

	failing, err := mgr.Begin("failing")
	require.NoError(t, err)
	require.NoError(t, failing.WriteFile(filepath.Join(blocker, "agent.md"), []byte("agent")))
	require.NoError(t, failing.WriteFile(db, []byte("stale")))
	require.Error(t, failing.Commit())
	require.NoError(t, failing.Rollback())

	later, err := mgr.Begin("later")
	require.NoError(t, err)
	require.NoError(t, later.WriteFile(db, []byte("fresh")))
	require.NoError(t, later.Commit())

	require.NoError(t, os.Remove(blocker))
	require.NoError(t, txadapter.New(stagingRoot).RecoverPending())

	got, err := os.ReadFile(db)
	require.NoError(t, err)
	require.Equal(t, "fresh", string(got))
	entries, err := os.ReadDir(stagingRoot)
	require.NoError(t, err)
	require.Empty(t, entries)
}
