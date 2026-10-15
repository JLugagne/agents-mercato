package cfgadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore"
)

var _ statestore.StateStore = (*StateStoreAdapter)(nil)

// StateStoreAdapter persists sync-state.json. SetMarketSyncDirty and
// SetMarketSyncClean serialize their read-modify-write across goroutines and
// processes through sync-state.lock and may return domain.ErrLockContention.
type StateStoreAdapter struct {
	mu    sync.Mutex
	locks fileLocker
}

func NewStateStore() *StateStoreAdapter { return &StateStoreAdapter{} }

func (s *StateStoreAdapter) LoadSyncState(cacheDir string) (domain.SyncState, error) {
	path := filepath.Join(cacheDir, "sync-state.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return domain.SyncState{
			Version: 1,
			Markets: make(map[string]domain.MarketSyncState),
		}, nil
	}
	if err != nil {
		return domain.SyncState{}, fmt.Errorf("read sync state: %w", err)
	}
	var state domain.SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		return domain.SyncState{}, fmt.Errorf("parse sync state: %w", err)
	}
	if state.Markets == nil {
		state.Markets = make(map[string]domain.MarketSyncState)
	}
	return state, nil
}

func (s *StateStoreAdapter) SaveSyncState(cacheDir string, state domain.SyncState) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(cacheDir, "sync-state.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sync state: %w", err)
	}
	return writeFileAtomic(path, data, 0600)
}

func (s *StateStoreAdapter) SetMarketSyncDirty(cacheDir string, market string) error {
	return s.updateMarket(cacheDir, market, func(ms *domain.MarketSyncState) {
		ms.Status = "dirty"
	})
}

func (s *StateStoreAdapter) SetMarketSyncClean(cacheDir string, market string, newSHA string) error {
	return s.updateMarket(cacheDir, market, func(ms *domain.MarketSyncState) {
		ms.Status = "clean"
		ms.LastSyncedSHA = newSHA
		ms.LastSyncedAt = time.Now().UTC()
	})
}

func (s *StateStoreAdapter) updateMarket(cacheDir, market string, mutate func(*domain.MarketSyncState)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lockPath := filepath.Join(cacheDir, "sync-state.lock")
	if err := s.locks.lock(lockPath, 5*time.Second, 20*time.Millisecond); err != nil {
		return err
	}
	state, err := s.LoadSyncState(cacheDir)
	if err == nil {
		ms := state.Markets[market]
		mutate(&ms)
		state.Markets[market] = ms
		err = s.SaveSyncState(cacheDir, state)
	}
	return errors.Join(err, s.locks.unlock(lockPath))
}
