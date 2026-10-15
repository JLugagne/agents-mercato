package cfgadapter_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
)

func bigDB() domain.InstallDatabase {
	db := domain.InstallDatabase{SchemaVersion: domain.InstallSchemaVersion}
	for i := range 500 {
		db.AddOrUpdatePackage("m", fmt.Sprintf("p/%d", i), "v1",
			domain.InstalledFiles{Agents: []string{"a.md"}},
			domain.InstalledLocation{Path: "/proj", Type: domain.RuntimeTypeClaudeCode})
	}
	return db
}

func TestInstallDB_ConcurrentLoadNeverSeesPartialSave(t *testing.T) {
	dir := t.TempDir()
	idb := cfgadapter.NewInstallDB()
	db := bigDB()
	require.NoError(t, idb.Save(dir, db))

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Go(func() {
		for !stop.Load() {
			_ = idb.Save(dir, db)
		}
	})
	var loadErr error
	for range 20 {
		if _, err := idb.Load(dir); err != nil {
			loadErr = err
			break
		}
	}
	stop.Store(true)
	wg.Wait()
	require.NoError(t, loadErr)
}

func TestStateStore_ConcurrentLoadNeverSeesPartialSave(t *testing.T) {
	dir := t.TempDir()
	st := cfgadapter.NewStateStore()
	state := domain.SyncState{Version: 1, Markets: map[string]domain.MarketSyncState{}}
	for i := range 1000 {
		state.Markets[fmt.Sprintf("market-%d", i)] = domain.MarketSyncState{LastSyncedSHA: "abcdef0123456789"}
	}
	require.NoError(t, st.SaveSyncState(dir, state))

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Go(func() {
		for !stop.Load() {
			_ = st.SaveSyncState(dir, state)
		}
	})
	var loadErr error
	for range 20 {
		if _, err := st.LoadSyncState(dir); err != nil {
			loadErr = err
			break
		}
	}
	stop.Store(true)
	wg.Wait()
	require.NoError(t, loadErr)
}

func TestConfigStore_AddMarketCreatesMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mct", "config.yml")
	cs := cfgadapter.NewConfigStore()
	require.NoError(t, cs.AddMarket(path, domain.MarketConfig{Name: "m", URL: "https://x", Branch: "main"}))

	cfg, err := cs.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Markets, 1)
}
