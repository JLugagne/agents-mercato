package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func TestSetConfigField_UnknownToolIsRejected(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	a := f.toolApp(t, t.TempDir())

	err := a.SetConfigField("tools.cursr", "true")

	require.True(t, errors.Is(err, domain.ErrUnknownTool), "got %v", err)
	require.NoError(t, a.SetConfigField("tools.cursor", "true"))
}

func TestAdd_UnknownToolIsRejected(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	a := f.toolApp(t, t.TempDir())

	_, err := a.Add(agentRef, service.AddOpts{Tools: map[string]bool{"opencod": true}})

	require.True(t, errors.Is(err, domain.ErrUnknownTool), "got %v", err)
}
