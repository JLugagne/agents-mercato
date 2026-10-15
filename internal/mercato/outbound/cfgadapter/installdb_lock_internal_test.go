package cfgadapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func fastInstallDB() *InstallDBAdapter {
	a := NewInstallDB()
	a.lockTimeout = 300 * time.Millisecond
	a.lockPoll = time.Millisecond
	return a
}

func TestInstallDBLock_EmptyLockFileIsNotPermanentContention(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "installed.lock")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	a := fastInstallDB()
	if err := a.Lock(dir); err != nil {
		t.Fatalf("Lock with an empty leftover lock file: %v", err)
	}
	if err := a.Unlock(dir); err != nil {
		t.Fatal(err)
	}
}

func TestInstallDBLock_UnlockDoesNotReleaseForeignLock(t *testing.T) {
	dir := t.TempDir()
	owner := fastInstallDB()
	other := fastInstallDB()
	third := fastInstallDB()

	if err := owner.Lock(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Unlock(dir) }()

	if err := other.Unlock(dir); err != nil {
		t.Fatalf("Unlock of a lock not held: %v", err)
	}

	err := third.Lock(dir)
	if err == nil {
		_ = third.Unlock(dir)
		t.Fatal("third adapter acquired a lock still held by owner")
	}
	if !errors.Is(err, domain.ErrLockContention) {
		t.Fatalf("error = %v, want ErrLockContention", err)
	}
}

func TestInstallDBLock_StaleTakeoverIsExclusive(t *testing.T) {
	for iter := range 100 {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "installed.lock")
		if err := os.WriteFile(lockPath, fmt.Appendf(nil, "%d", 999999999), 0o600); err != nil {
			t.Fatal(err)
		}

		var holders, violations atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range 4 {
			wg.Go(func() {
				a := fastInstallDB()
				a.lockTimeout = 2 * time.Second
				<-start
				if err := a.Lock(dir); err != nil {
					t.Errorf("Lock: %v", err)
					return
				}
				if holders.Add(1) > 1 {
					violations.Add(1)
				}
				time.Sleep(2 * time.Millisecond)
				holders.Add(-1)
				_ = a.Unlock(dir)
			})
		}
		close(start)
		wg.Wait()
		if v := violations.Load(); v > 0 {
			t.Fatalf("iteration %d: %d overlapping lock holders", iter, v)
		}
	}
}

func TestStateStore_ConcurrentSetMarketSyncKeepsEveryUpdate(t *testing.T) {
	dir := t.TempDir()
	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			s := NewStateStore()
			market := fmt.Sprintf("m%d", i)
			if err := s.SetMarketSyncDirty(dir, market); err != nil {
				t.Errorf("SetMarketSyncDirty: %v", err)
			}
			if err := s.SetMarketSyncClean(dir, market, "sha"); err != nil {
				t.Errorf("SetMarketSyncClean: %v", err)
			}
		})
	}
	wg.Wait()

	state, err := NewStateStore().LoadSyncState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Markets) != n {
		t.Fatalf("got %d markets, want %d (lost updates)", len(state.Markets), n)
	}
	for name, ms := range state.Markets {
		if ms.Status != "clean" {
			t.Errorf("%s status = %q, want clean", name, ms.Status)
		}
	}
}
