package txadapter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCommit_PromotesAllStagedWritesAtomically(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(t.TempDir(), "project")
	mgr := New(filepath.Join(root, "staging"))

	tx, err := mgr.Begin("test")
	if err != nil {
		t.Fatal(err)
	}

	a := filepath.Join(dest, "a.md")
	b := filepath.Join(dest, "sub", "b.md")
	if err := tx.WriteFile(a, []byte("alpha")); err != nil {
		t.Fatal(err)
	}
	if err := tx.WriteFile(b, []byte("bravo")); err != nil {
		t.Fatal(err)
	}

	// Files must NOT exist yet — they live in the staging dir.
	if _, err := os.Stat(a); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file a leaked before commit")
	}
	if _, err := os.Stat(b); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file b leaked before commit")
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(a); string(got) != "alpha" {
		t.Errorf("a: got %q", got)
	}
	if got, _ := os.ReadFile(b); string(got) != "bravo" {
		t.Errorf("b: got %q", got)
	}
}

func TestRollback_LeavesNoTrace(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(t.TempDir(), "project")
	stagingRoot := filepath.Join(root, "staging")
	mgr := New(stagingRoot)

	tx, err := mgr.Begin("test")
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(dest, "a.md")
	if err := tx.WriteFile(a, []byte("alpha")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(a); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file a should not exist after rollback")
	}
	entries, _ := os.ReadDir(stagingRoot)
	if len(entries) != 0 {
		t.Errorf("staging root should be empty, got %d entries", len(entries))
	}
}

func TestRecoverPending_ReplaysCommittingState(t *testing.T) {
	stagingRoot := t.TempDir()
	dest := t.TempDir()

	// Hand-craft a "committing" staging dir as if a previous run crashed
	// after writing the manifest+state but before promoting files.
	txDir := filepath.Join(stagingRoot, "crashed-tx")
	if err := os.MkdirAll(filepath.Join(txDir, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	stagedFile := filepath.Join(txDir, "files", "0")
	if err := os.WriteFile(stagedFile, []byte("recovered"), 0o644); err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(dest, "out.md")
	manifest := []byte(`{"writes":[{"final":"` + finalPath + `","slot":"0"}],"deletes":null}`)
	if err := os.WriteFile(filepath.Join(txDir, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(txDir, "state"), []byte("committing"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := New(stagingRoot)
	if err := mgr.RecoverPending(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "recovered" {
		t.Errorf("got %q", got)
	}
	if _, err := os.Stat(txDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staging dir should be cleaned up after recovery")
	}
}

func TestRecoverPending_DiscardsOpenStaging(t *testing.T) {
	stagingRoot := t.TempDir()
	txDir := filepath.Join(stagingRoot, "halfwritten")
	if err := os.MkdirAll(filepath.Join(txDir, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(txDir, "state"), []byte("open"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := New(stagingRoot)
	if err := mgr.RecoverPending(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(txDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("open staging dir should be discarded")
	}
}

func TestDeleteAndDeleteAll_AppliedOnCommit(t *testing.T) {
	stagingRoot := t.TempDir()
	dest := t.TempDir()
	keep := filepath.Join(dest, "keep.md")
	gone := filepath.Join(dest, "gone.md")
	dir := filepath.Join(dest, "subdir")
	dirChild := filepath.Join(dir, "x.md")
	for _, p := range []string{keep, gone} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dirChild, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := New(stagingRoot)
	tx, err := mgr.Begin("test")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.DeleteFile(gone); err != nil {
		t.Fatal(err)
	}
	if err := tx.DeleteAll(dir); err != nil {
		t.Fatal(err)
	}

	// Pre-commit: nothing changes yet.
	if _, err := os.Stat(gone); err != nil {
		t.Fatal("gone removed too early")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("dir removed too early")
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("gone should be deleted")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir should be removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep should still exist: %v", err)
	}
}

func TestClosedTxRejectsFurtherCalls(t *testing.T) {
	mgr := New(t.TempDir())
	tx, err := mgr.Begin("test")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx.WriteFile("/tmp/x", nil); err == nil {
		t.Error("WriteFile after Commit should fail")
	}
	if err := tx.Commit(); err == nil {
		t.Error("Commit after Commit should fail")
	}
}

func TestRecoverPending_ResumesFromProgress(t *testing.T) {
	stagingRoot := t.TempDir()
	dest := t.TempDir()
	skillDir := filepath.Join(dest, "skills", "foo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	promoted := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(promoted, []byte("promoted"), 0o644); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(dest, "installed.json")

	txDir := filepath.Join(stagingRoot, "crashed-tx")
	if err := os.MkdirAll(filepath.Join(txDir, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(txDir, "files", "2"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	man := manifest{Ops: []op{
		{Kind: opDeleteAll, Final: skillDir},
		{Kind: opWrite, Final: promoted, Slot: "1"},
		{Kind: opWrite, Final: pending, Slot: "2"},
	}}
	if err := writeManifest(txDir, man); err != nil {
		t.Fatal(err)
	}
	if err := writeProgress(txDir, 2); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(txDir, "state"), []byte("committing"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := New(stagingRoot).RecoverPending(); err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(promoted); err != nil || string(got) != "promoted" {
		t.Errorf("already-applied write was undone: %q, %v", got, err)
	}
	if got, err := os.ReadFile(pending); err != nil || string(got) != "db" {
		t.Errorf("pending write not replayed: %q, %v", got, err)
	}
	if _, err := os.Stat(txDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staging dir should be cleaned up after recovery")
	}
}
