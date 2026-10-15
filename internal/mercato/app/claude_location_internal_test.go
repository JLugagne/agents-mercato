package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func TestDropHookFromPackage_UsesClaudeLocationWhenToolLocationComesFirst(t *testing.T) {
	db := domain.InstallDatabase{Markets: []domain.InstalledMarket{{Market: "m", Packages: []domain.InstalledPackage{{
		Profile: "dev/go",
		Files:   domain.InstalledFiles{Hooks: []string{"x.json"}, Agents: []string{"a.md"}},
		Locations: []domain.InstalledLocation{
			{Path: "/p", Type: "codex", Files: []domain.InstalledFile{{Path: ".codex/hooks.json#hooks/PreToolUse", Source: "dev/go/hooks/x.json"}}},
			{Path: "/p", Type: domain.RuntimeTypeClaudeCode, Files: []domain.InstalledFile{{Path: ".claude/settings.json#hooks/x.json"}, {Path: ".claude/agents/a.md"}}},
		},
	}}}}}
	a := &App{}

	require.NoError(t, a.dropHookFromPackage(&db, "m", "dev/go", "/p", "x.json"))

	pkg := db.FindPackage("m", "dev/go")
	claude := findLocationByPathAndType(pkg, "/p", domain.RuntimeTypeClaudeCode)
	require.Equal(t, []domain.InstalledFile{{Path: ".claude/agents/a.md"}}, claude.Files)
}
