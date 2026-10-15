package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/tx"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
)

type recordingTxManager struct{ recovered int }

func (m *recordingTxManager) Begin(string) (tx.Tx, error) { return nil, nil }
func (m *recordingTxManager) RecoverPending() error       { m.recovered++; return nil }

type contendedInstallDB struct{ *cfgadapter.InstallDBAdapter }

func (contendedInstallDB) Lock(string) error { return domain.ErrLockContention }

func TestRecoverPendingTx_SkipsWhileAnotherProcessHoldsTheLock(t *testing.T) {
	txm := &recordingTxManager{}
	a := New(nil, nil, nil, nil, contendedInstallDB{cfgadapter.NewInstallDB()}, "", t.TempDir(), WithTxManager(txm))

	require.NoError(t, a.RecoverPendingTx())
	require.Zero(t, txm.recovered)
}

func TestRecoverPendingTx_RunsUnderLock(t *testing.T) {
	txm := &recordingTxManager{}
	cacheDir := t.TempDir()
	idb := cfgadapter.NewInstallDB()
	a := New(nil, nil, nil, nil, idb, "", cacheDir, WithTxManager(txm))

	require.NoError(t, a.RecoverPendingTx())
	require.Equal(t, 1, txm.recovered)
	require.NoError(t, idb.Lock(cacheDir))
	require.NoError(t, idb.Unlock(cacheDir))
}
