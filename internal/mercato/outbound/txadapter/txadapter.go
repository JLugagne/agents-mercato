// Package txadapter implements the tx.Manager port using a per-operation
// staging directory under <stagingRoot>/<txID>/ on the local filesystem.
//
// Layout for an in-progress transaction:
//
//	<stagingRoot>/<txID>/
//	  manifest.json   ← ordered list of staged writes, deletes and delete-alls
//	  state           ← "open" → "committing" → "done" (or "failed")
//	  progress        ← number of operations already applied
//	  files/<n>       ← staged content for the n-th operation
//
// Commit protocol (recoverable):
//  1. Write manifest.json + transition state from "open" to "committing".
//  2. Apply every operation in call order: writes are promoted via
//     os.Rename (copy+remove across filesystems), deletes and delete-alls
//     are applied in place. progress is advanced after each operation.
//  3. Transition state to "done" and remove the staging directory.
//
// A Commit that fails while the process is alive marks the transaction
// "failed" and removes it: the caller has seen the error and later
// operations may already have rewritten the same targets, so replaying it
// would clobber newer data.
//
// Recovery on next startup: a "committing" tx (the process died mid-commit)
// resumes from progress; every other state is discarded.
package txadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/tx"
)

const (
	stateOpen       = "open"
	stateCommitting = "committing"
	stateDone       = "done"

	stateFailed = "failed"

	manifestName = "manifest.json"
	stateName    = "state"
	progressName = "progress"
	filesDir     = "files"

	opWrite     = "write"
	opDelete    = "delete"
	opDeleteAll = "delete_all"
)

// Manager is the disk-backed implementation of tx.Manager.
type Manager struct {
	root string
}

var _ tx.Manager = (*Manager)(nil)

// New returns a Manager whose staging directories live under stagingRoot.
// The directory is created on demand.
func New(stagingRoot string) *Manager {
	return &Manager{root: stagingRoot}
}

// Begin opens a new transaction. The op string is included in the staging
// directory name purely for diagnostics.
func (m *Manager) Begin(op string) (tx.Tx, error) {
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return nil, errors.Join(errors.New("tx: create staging root"), err)
	}
	id := newTxID(op)
	dir := filepath.Join(m.root, id)
	if err := os.MkdirAll(filepath.Join(dir, filesDir), 0o755); err != nil {
		return nil, errors.Join(errors.New("tx: create staging dir"), err)
	}
	t := &diskTx{dir: dir, id: id}
	if err := t.writeState(stateOpen); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return t, nil
}

// RecoverPending reconciles any leftover staging directories. Called at
// startup before the application performs any new writes.
func (m *Manager) RecoverPending() error {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.Join(errors.New("tx: list staging root"), err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(m.root, e.Name())
		if err := recoverOne(dir); err != nil {
			// Best-effort: log via stderr, keep going. A dangling staging
			// dir should never block normal operation.
			fmt.Fprintf(os.Stderr, "mct: tx recovery failed for %s: %v\n", dir, err)
		}
	}
	return nil
}

func recoverOne(dir string) error {
	st, err := os.ReadFile(filepath.Join(dir, stateName))
	if err != nil {
		// No state file at all — discard.
		return os.RemoveAll(dir)
	}
	switch string(st) {
	case stateCommitting:
		// Mid-commit crash: replay file promotions + deletes. We do NOT
		// rerun OnCommit hooks during recovery — they live in the
		// application process and the install database update is handled
		// by mct's normal startup paths.
		man, err := readManifest(dir)
		if err != nil {
			return err
		}
		if err := applyOps(dir, man.operations(), readProgress(dir)); err != nil {
			return err
		}
		return os.RemoveAll(dir)
	default:
		// stateOpen or anything unknown: discard.
		return os.RemoveAll(dir)
	}
}

type diskTx struct {
	mu     sync.Mutex
	dir    string
	id     string
	ops    []op
	closed bool
}

type op struct {
	Kind  string `json:"kind"`
	Final string `json:"final"`
	Slot  string `json:"slot,omitempty"`
}

type writeEntry struct {
	Final string `json:"final"`
	Slot  string `json:"slot"`
}

type manifest struct {
	Ops        []op         `json:"ops,omitempty"`
	Writes     []writeEntry `json:"writes,omitempty"`
	Deletes    []string     `json:"deletes,omitempty"`
	DeleteAlls []string     `json:"delete_alls,omitempty"`
}

func (m manifest) operations() []op {
	if len(m.Ops) > 0 {
		return m.Ops
	}
	ops := make([]op, 0, len(m.Writes)+len(m.Deletes)+len(m.DeleteAlls))
	for _, w := range m.Writes {
		ops = append(ops, op{Kind: opWrite, Final: w.Final, Slot: w.Slot})
	}
	for _, p := range m.Deletes {
		ops = append(ops, op{Kind: opDelete, Final: p})
	}
	for _, p := range m.DeleteAlls {
		ops = append(ops, op{Kind: opDeleteAll, Final: p})
	}
	return ops
}

func (t *diskTx) WriteFile(path string, content []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return tx.ErrTxClosed
	}
	slot := strconv.Itoa(len(t.ops))
	full := filepath.Join(t.dir, filesDir, slot)
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return errors.Join(fmt.Errorf("tx: stage write %s", path), err)
	}
	t.ops = append(t.ops, op{Kind: opWrite, Final: path, Slot: slot})
	return nil
}

func (t *diskTx) DeleteFile(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return tx.ErrTxClosed
	}
	t.ops = append(t.ops, op{Kind: opDelete, Final: path})
	return nil
}

func (t *diskTx) DeleteAll(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return tx.ErrTxClosed
	}
	t.ops = append(t.ops, op{Kind: opDeleteAll, Final: path})
	return nil
}

func (t *diskTx) Commit() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return tx.ErrTxClosed
	}
	t.closed = true
	ops := t.ops
	t.mu.Unlock()

	if err := writeManifest(t.dir, manifest{Ops: ops}); err != nil {
		return t.abandon(err)
	}
	if err := t.writeState(stateCommitting); err != nil {
		return t.abandon(err)
	}
	if err := applyOps(t.dir, ops, 0); err != nil {
		return t.abandon(err)
	}
	if err := t.writeState(stateDone); err != nil {
		return err
	}
	return os.RemoveAll(t.dir)
}

func (t *diskTx) abandon(cause error) error {
	return errors.Join(cause, t.writeState(stateFailed), os.RemoveAll(t.dir))
}

func (t *diskTx) Rollback() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	return os.RemoveAll(t.dir)
}

func (t *diskTx) writeState(s string) error {
	tmp := filepath.Join(t.dir, stateName+".tmp")
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return errors.Join(errors.New("tx: write state"), err)
	}
	if err := os.Rename(tmp, filepath.Join(t.dir, stateName)); err != nil {
		return errors.Join(errors.New("tx: rename state"), err)
	}
	return nil
}

func writeManifest(dir string, m manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return errors.Join(errors.New("tx: marshal manifest"), err)
	}
	tmp := filepath.Join(dir, manifestName+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return errors.Join(errors.New("tx: write manifest"), err)
	}
	return os.Rename(tmp, filepath.Join(dir, manifestName))
}

func readManifest(dir string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return m, errors.Join(errors.New("tx: read manifest"), err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, errors.Join(errors.New("tx: parse manifest"), err)
	}
	return m, nil
}

func applyOps(dir string, ops []op, from int) error {
	for i := from; i < len(ops); i++ {
		if err := applyOp(dir, ops[i]); err != nil {
			return err
		}
		if err := writeProgress(dir, i+1); err != nil {
			return err
		}
	}
	return nil
}

func applyOp(dir string, o op) error {
	switch o.Kind {
	case opWrite:
		return applyWrite(dir, o)
	case opDelete:
		if err := os.Remove(o.Final); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(fmt.Errorf("tx: delete %s", o.Final), err)
		}
	case opDeleteAll:
		if err := os.RemoveAll(o.Final); err != nil {
			return errors.Join(fmt.Errorf("tx: delete-all %s", o.Final), err)
		}
	default:
		return fmt.Errorf("tx: unknown operation %q", o.Kind)
	}
	return nil
}

func applyWrite(dir string, o op) error {
	src := filepath.Join(dir, filesDir, o.Slot)
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(o.Final), 0o755); err != nil {
		return errors.Join(fmt.Errorf("tx: mkdir %s", filepath.Dir(o.Final)), err)
	}
	err := os.Rename(src, o.Final)
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return errors.Join(fmt.Errorf("tx: promote %s", o.Final), err)
	}
	if cerr := copyAndRemove(src, o.Final); cerr != nil {
		return errors.Join(fmt.Errorf("tx: cross-fs promote %s", o.Final), cerr)
	}
	return nil
}

func writeProgress(dir string, n int) error {
	tmp := filepath.Join(dir, progressName+".tmp")
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(n)), 0o644); err != nil {
		return errors.Join(errors.New("tx: write progress"), err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, progressName)); err != nil {
		return errors.Join(errors.New("tx: rename progress"), err)
	}
	return nil
}

func readProgress(dir string) int {
	data, err := os.ReadFile(filepath.Join(dir, progressName))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(string(data))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func copyAndRemove(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp := dst + ".mct-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Remove(src)
}

func newTxID(op string) string {
	return fmt.Sprintf("%d-%d-%s", time.Now().UnixNano(), os.Getpid(), sanitize(op))
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s) && i < 32; i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "op"
	}
	return string(out)
}
