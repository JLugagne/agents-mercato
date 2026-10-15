package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
)

type racingInstallDB struct {
	*cfgadapter.InstallDBAdapter
	race func()
}

func (r *racingInstallDB) Lock(cacheDir string) error {
	if r.race != nil {
		race := r.race
		r.race = nil
		race()
	}
	return r.InstallDBAdapter.Lock(cacheDir)
}

func staleSetup(t *testing.T) (*App, *racingInstallDB) {
	t.Helper()
	f := newSyncFixture(t, marketFiles())
	p1, p2 := t.TempDir(), t.TempDir()
	a1, a2 := f.app(t, p1), f.app(t, p2)
	_, err := a1.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	_, err = a2.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(p2))

	idb := &racingInstallDB{InstallDBAdapter: cfgadapter.NewInstallDB()}
	idb.race = func() {
		inner := cfgadapter.NewInstallDB()
		require.NoError(t, inner.Lock(a1.cacheDir))
		db, err := inner.Load(a1.cacheDir)
		require.NoError(t, err)
		db.Markets = append(db.Markets, domain.InstalledMarket{Market: "concurrent", Packages: []domain.InstalledPackage{{Profile: "x/y", Version: "v", Locations: []domain.InstalledLocation{{Path: p1}}}}})
		require.NoError(t, inner.Save(a1.cacheDir, db))
		require.NoError(t, inner.Unlock(a1.cacheDir))
	}
	a1.idb = idb
	return a1, idb
}

func requireConcurrentWriteKept(t *testing.T, a *App) {
	t.Helper()
	db, err := a.idb.Load(a.cacheDir)
	require.NoError(t, err)
	require.NotNil(t, db.FindMarket("concurrent"), "a write made by another process was overwritten")
}

func TestCheck_StaleCleanupKeepsConcurrentWrites(t *testing.T) {
	a, _ := staleSetup(t)
	_, err := a.Check(service.CheckOpts{})
	require.NoError(t, err)
	requireConcurrentWriteKept(t, a)
}

func TestList_StaleCleanupKeepsConcurrentWrites(t *testing.T) {
	a, _ := staleSetup(t)
	_, err := a.List(service.ListOpts{})
	require.NoError(t, err)
	requireConcurrentWriteKept(t, a)
}
