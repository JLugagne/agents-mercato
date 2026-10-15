package cfgadapter

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

type fileLocker struct {
	mu   sync.Mutex
	held map[string]*os.File
}

func (l *fileLocker) lock(path string, timeout, poll time.Duration) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		f, err := tryLockPath(path)
		if err != nil {
			return err
		}
		if f != nil {
			l.mu.Lock()
			if l.held == nil {
				l.held = make(map[string]*os.File)
			}
			l.held[path] = f
			l.mu.Unlock()
			return nil
		}
		if !time.Now().Before(deadline) {
			return domain.ErrLockContention
		}
		time.Sleep(poll)
	}
}

func (l *fileLocker) unlock(path string) error {
	l.mu.Lock()
	f, ok := l.held[path]
	delete(l.held, path)
	l.mu.Unlock()
	if !ok {
		return nil
	}
	return errors.Join(releaseLockPath(path), unlockFile(f), f.Close())
}

func tryLockPath(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ok, err := lockFileNonBlocking(f)
	if err != nil || !ok {
		return nil, errors.Join(err, f.Close())
	}
	if !stillLinked(f, path) {
		return nil, errors.Join(unlockFile(f), f.Close())
	}
	if err := writeLockOwner(f); err != nil {
		return nil, errors.Join(err, unlockFile(f), f.Close())
	}
	return f, nil
}

func stillLinked(f *os.File, path string) bool {
	held, err := f.Stat()
	if err != nil {
		return false
	}
	current, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(held, current)
}

func writeLockOwner(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	return err
}
