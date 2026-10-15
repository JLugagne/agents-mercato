package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func TestInstalledPackage_VersionIsTrackedPerLocation(t *testing.T) {
	var db domain.InstallDatabase
	loc := func(p string) domain.InstalledLocation {
		return domain.InstalledLocation{Path: p, Type: domain.RuntimeTypeClaudeCode}
	}
	db.AddOrUpdatePackage("m", "dev/go", "v1", domain.InstalledFiles{Agents: []string{"a.md"}}, loc("/p1"))
	db.AddOrUpdatePackage("m", "dev/go", "v1", domain.InstalledFiles{Agents: []string{"a.md"}}, loc("/p2"))

	pkg := db.FindPackage("m", "dev/go")
	pkg.SetVersionAt("/p1", "v2")

	require.Equal(t, "v2", pkg.VersionAt("/p1"))
	require.Equal(t, "v1", pkg.VersionAt("/p2"))
}

func TestInstalledPackage_LegacyLocationsKeepPackageVersion(t *testing.T) {
	pkg := domain.InstalledPackage{
		Version: "v1",
		Locations: []domain.InstalledLocation{
			{Path: "/p1", Type: domain.RuntimeTypeClaudeCode},
			{Path: "/p2", Type: domain.RuntimeTypeClaudeCode},
		},
	}
	pkg.SetVersionAt("/p1", "v2")

	require.Equal(t, "v2", pkg.VersionAt("/p1"))
	require.Equal(t, "v1", pkg.VersionAt("/p2"))
}

func TestAddOrUpdatePackage_SiblingAddKeepsLocationVersion(t *testing.T) {
	var db domain.InstallDatabase
	loc := domain.InstalledLocation{Path: "/p1", Type: domain.RuntimeTypeClaudeCode}
	db.AddOrUpdatePackage("m", "dev/go", "v1", domain.InstalledFiles{Agents: []string{"a.md"}}, loc)
	db.AddOrUpdatePackage("m", "dev/go", "v2", domain.InstalledFiles{Agents: []string{"a.md", "b.md"}}, loc)

	require.Equal(t, "v1", db.FindPackage("m", "dev/go").VersionAt("/p1"))
}
