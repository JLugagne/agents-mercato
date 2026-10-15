package commands

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func TestRefresh_FirstSyncWithoutOldSHA(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		refreshFn: func(service.RefreshOpts) ([]service.RefreshResult, error) {
			return []service.RefreshResult{{Market: "mkt", NewSHA: "def5678xxx"}}, nil
		},
	}
	out, err := runCmd(t, svc, "refresh")
	require.NoError(t, err)
	require.Contains(t, out, "def5678")
}

func TestSync_FirstSyncWithoutOldSHA(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		refreshFn: func(service.RefreshOpts) ([]service.RefreshResult, error) {
			return []service.RefreshResult{{Market: "mkt", NewSHA: "def5678xxx"}}, nil
		},
	}
	out, err := runCmd(t, svc, "sync", "--restore-none")
	require.NoError(t, err)
	require.Contains(t, out, "def5678")
}

func TestSync_AllKeepFlagIsForwarded(t *testing.T) {
	var got service.UpdateOpts
	svc := mockServices()
	svc.Sync = &stubSync{
		updateFn: func(opts service.UpdateOpts) ([]service.UpdateResult, error) {
			got = opts
			return nil, nil
		},
	}
	_, err := runCmd(t, svc, "sync", "--all-keep", "--restore-none")
	require.NoError(t, err)
	require.True(t, got.AllKeep)
}

func TestCheck_LocallyDeletedHasIndicator(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{
		checkFn: func(service.CheckOpts) ([]domain.EntryStatus, error) {
			return []domain.EntryStatus{{Ref: "mkt@agents/foo.md", State: domain.StateLocallyDeleted}}, nil
		},
	}
	out, err := runCmd(t, svc, "check")
	require.NoError(t, err)
	require.NotContains(t, out, "    mkt@agents/foo.md")
}
