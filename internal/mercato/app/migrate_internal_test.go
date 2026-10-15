package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func editDB(t *testing.T, a *App, edit func(db *domain.InstallDatabase)) {
	t.Helper()
	db, err := a.idb.Load(a.cacheDir)
	require.NoError(t, err)
	edit(&db)
	require.NoError(t, a.idb.Lock(a.cacheDir))
	require.NoError(t, a.idb.Save(a.cacheDir, db))
	require.NoError(t, a.idb.Unlock(a.cacheDir))
}

func addLegacyToolFile(t *testing.T, a *App, projectDir, profile, tool, rel, content string) {
	t.Helper()
	abs := filepath.Join(projectDir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	editDB(t, a, func(db *domain.InstallDatabase) {
		pkg := db.FindPackage(marketName, profile)
		require.NotNil(t, pkg)
		rec := domain.InstalledFile{Path: rel, XXH: xxhashHex([]byte(content))}
		if loc := findLocationByPathAndType(pkg, projectDir, tool); loc != nil {
			loc.Files = append(loc.Files, rec)
			return
		}
		pkg.Locations = append(pkg.Locations, domain.InstalledLocation{Path: projectDir, Type: tool, Files: []domain.InstalledFile{rec}})
	})
}

func makeLegacyFlatSkill(t *testing.T, a *App, projectDir, content string) {
	t.Helper()
	skillDir := filepath.Join(projectDir, ".claude/skills/flat")
	require.NoError(t, os.RemoveAll(skillDir))
	require.NoError(t, os.WriteFile(skillDir, []byte(content), 0o644))
	editDB(t, a, func(db *domain.InstallDatabase) {
		for mi := range db.Markets {
			for pi := range db.Markets[mi].Packages {
				pkg := &db.Markets[mi].Packages[pi]
				pkg.Files.FlatSkills = nil
				for li := range pkg.Locations {
					for fi := range pkg.Locations[li].Files {
						if pkg.Locations[li].Files[fi].Path == ".claude/skills/flat/SKILL.md" {
							pkg.Locations[li].Files[fi].Path = ".claude/skills/flat"
						}
					}
				}
			}
		}
	})
}

func migrationActions(results []service.MigrateResult) string {
	var out []string
	for _, r := range results {
		out = append(out, r.Actions...)
	}
	return strings.Join(out, "\n")
}

func TestMigrate_LegacyFlatSkillFileMovedKeepingLocalContent(t *testing.T) {
	_, a, p := installFlatSkill(t)
	makeLegacyFlatSkill(t, a, p, flatSkillBody("v1"))

	results, err := a.Migrate(service.MigrateOpts{})
	require.NoError(t, err)

	require.Contains(t, migrationActions(results), "skills/flat")
	require.Equal(t, flatSkillBody("v1"), readFile(t, filepath.Join(p, ".claude/skills/flat/SKILL.md")))
	require.Equal(t, domain.StateClean, stateOf(t, a, flatSkillRef))
}

func TestMigrate_LegacyFlatSkillLocalEditsArePreserved(t *testing.T) {
	_, a, p := installFlatSkill(t)
	makeLegacyFlatSkill(t, a, p, flatSkillBody("my edits"))

	_, err := a.Migrate(service.MigrateOpts{})
	require.NoError(t, err)

	require.Equal(t, flatSkillBody("my edits"), readFile(t, filepath.Join(p, ".claude/skills/flat/SKILL.md")))
	require.Equal(t, domain.StateDrift, stateOf(t, a, flatSkillRef))
}

func TestMigrate_LegacyHookEntriesConverted(t *testing.T) {
	f := newSyncFixture(t, twoHookMarketFiles("go vet ./..."))
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(goVetHookRef, service.AddOpts{})
	require.NoError(t, err)
	writeLegacyGoVetState(t, f, p, userLegacy+",")

	results, err := a.Migrate(service.MigrateOpts{})
	require.NoError(t, err)

	require.Contains(t, migrationActions(results), "settings.json")
	settings := readFile(t, filepath.Join(p, ".claude/settings.json"))
	require.NotContains(t, settings, "mct_id")
	require.Contains(t, settings, "legacy-user")
	require.Contains(t, settings, `"theme": "dark"`)
	require.FileExists(t, filepath.Join(p, ".claude/mct-hooks.json"))
	require.Equal(t, domain.StateClean, stateOf(t, a, goVetHookRef))
}

func TestMigrate_ToolOutputsRegeneratedAndObsoleteRemoved(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	mkdirs(t, p, ".cursor", ".opencode")
	a := f.toolApp(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	setTools(t, a, "cursor", "opencode")
	addLegacyToolFile(t, a, p, "dev/go", "cursor", ".cursor/rules/go-arch.mdc", "---\nalwaysApply: false\n---\nold rule\n")
	addLegacyToolFile(t, a, p, "dev/go", "opencode", ".opencode/agents/code-review.md", "---\ndescription: Go code reviewer\nmode: primary\n---\nold\n")
	addLegacyToolFile(t, a, p, "dev/go", "supermaven", ".supermavenrules", "# old\n")

	results, err := a.Migrate(service.MigrateOpts{})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	require.NoFileExists(t, filepath.Join(p, ".cursor/rules/go-arch.mdc"))
	require.NoFileExists(t, filepath.Join(p, ".supermavenrules"))
	agent := readFile(t, filepath.Join(p, ".opencode/agents/code-review.md"))
	require.Contains(t, agent, "mode: subagent")
	require.Contains(t, agent, "Review Go code for best practices.")
	loc := toolLocation(t, a, "dev/go", p, "opencode")
	require.NotNil(t, loc)
	for _, f := range loc.Files {
		require.NotEmpty(t, f.Source, f.Path)
	}
	require.Nil(t, toolLocation(t, a, "dev/go", p, "supermaven"))

	again, err := a.Migrate(service.MigrateOpts{})
	require.NoError(t, err)
	require.Empty(t, again)
}

func TestMigrate_LocallyModifiedLegacyToolFileIsKept(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	mkdirs(t, p, ".opencode")
	a := f.toolApp(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	setTools(t, a, "opencode")
	agentPath := filepath.Join(p, ".opencode/agents/code-review.md")
	addLegacyToolFile(t, a, p, "dev/go", "opencode", ".opencode/agents/code-review.md", "---\nmode: primary\n---\nold\n")
	require.NoError(t, os.WriteFile(agentPath, []byte("my own agent\n"), 0o644))

	results, err := a.Migrate(service.MigrateOpts{})
	require.NoError(t, err)

	require.Equal(t, "my own agent\n", readFile(t, agentPath))
	var warnings []string
	for _, r := range results {
		warnings = append(warnings, r.Warnings...)
	}
	require.Contains(t, strings.Join(warnings, "\n"), ".opencode/agents/code-review.md")
}

func TestMigrate_DryRunWritesNothing(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	mkdirs(t, p, ".cursor")
	a := f.toolApp(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	setTools(t, a, "cursor")
	addLegacyToolFile(t, a, p, "dev/go", "cursor", ".cursor/rules/go-arch.mdc", "old rule\n")

	results, err := a.Migrate(service.MigrateOpts{DryRun: true})
	require.NoError(t, err)

	require.Contains(t, migrationActions(results), ".cursor/rules/go-arch.mdc")
	require.FileExists(t, filepath.Join(p, ".cursor/rules/go-arch.mdc"))
	require.NotNil(t, toolLocation(t, a, "dev/go", p, "cursor"))
}

func TestUpdate_MigratesCurrentProjectWithoutUpstreamChange(t *testing.T) {
	f := newSyncFixture(t, marketFiles())
	p := t.TempDir()
	mkdirs(t, p, ".cursor")
	a := f.toolApp(t, p)
	_, err := a.Add(agentRef, service.AddOpts{})
	require.NoError(t, err)
	setTools(t, a, "cursor")
	addLegacyToolFile(t, a, p, "dev/go", "cursor", ".cursor/rules/go-arch.mdc", "old rule\n")

	results, err := a.Update(service.UpdateOpts{})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(p, ".cursor/rules/go-arch.mdc"))
	var actions []string
	for _, r := range results {
		actions = append(actions, r.Action)
	}
	require.Contains(t, actions, "migrated")
}
