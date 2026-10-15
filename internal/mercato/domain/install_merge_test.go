package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func TestMergeLocationFiles_HookPatchesOnSameAnchorAreKeptPerSource(t *testing.T) {
	anchor := ".codex/hooks.json#hooks/PreToolUse"
	existing := []domain.InstalledFile{{Path: anchor, XXH: "1", Source: "hooks/a.json", Data: json.RawMessage(`{"a":1}`)}}
	incoming := []domain.InstalledFile{{Path: anchor, XXH: "2", Source: "hooks/b.json", Data: json.RawMessage(`{"b":1}`)}}

	got := domain.MergeLocationFiles(existing, incoming)

	require.Len(t, got, 2)
}

func TestMergeLocationFiles_ReplacesWholeRecord(t *testing.T) {
	existing := []domain.InstalledFile{{Path: ".pi/prompts/x.md", XXH: "1", Source: "old"}}
	incoming := []domain.InstalledFile{{Path: ".pi/prompts/x.md", XXH: "2", Source: "commands/x.md"}}

	got := domain.MergeLocationFiles(existing, incoming)

	require.Equal(t, incoming, got)
}
