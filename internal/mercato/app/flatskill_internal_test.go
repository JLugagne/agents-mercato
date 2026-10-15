package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

const flatSkillRef = domain.MctRef(marketName + "@skills/flat.md")

func flatSkillBody(v string) string {
	return "---\ndescription: \"flat\"\n---\n" + v + "\n"
}

func installFlatSkill(t *testing.T) (*syncFixture, *App, string) {
	t.Helper()
	f := newSyncFixture(t, map[string]string{"skills/flat.md": flatSkillBody("v1")})
	p := t.TempDir()
	a := f.app(t, p)
	_, err := a.Add(flatSkillRef, service.AddOpts{})
	require.NoError(t, err)
	return f, a, p
}

func TestAdd_FlatSkillInstalledAsSkillDirectory(t *testing.T) {
	_, a, p := installFlatSkill(t)

	skillMd := filepath.Join(p, ".claude/skills/flat/SKILL.md")
	require.FileExists(t, skillMd)
	require.Contains(t, readFile(t, skillMd), "v1")
	require.Equal(t, domain.StateClean, stateOf(t, a, flatSkillRef))
}

func TestCheck_FlatSkillLocalEditIsDrift(t *testing.T) {
	_, a, p := installFlatSkill(t)

	require.NoError(t, os.WriteFile(filepath.Join(p, ".claude/skills/flat/SKILL.md"), []byte(flatSkillBody("edited")), 0o644))
	require.Equal(t, domain.StateDrift, stateOf(t, a, flatSkillRef))
}

func TestUpdate_FlatSkillWritesSkillMd(t *testing.T) {
	f, a, p := installFlatSkill(t)

	f.push(t, a, map[string]string{"skills/flat.md": flatSkillBody("v2")})
	_, err := a.Update(service.UpdateOpts{})
	require.NoError(t, err)

	require.Contains(t, readFile(t, filepath.Join(p, ".claude/skills/flat/SKILL.md")), "v2")
	require.Equal(t, domain.StateClean, stateOf(t, a, flatSkillRef))
}

func TestUpdate_FlatSkillMigratesLegacyExtensionlessFile(t *testing.T) {
	f, a, p := installFlatSkill(t)

	skillDir := filepath.Join(p, ".claude/skills/flat")
	require.NoError(t, os.RemoveAll(skillDir))
	require.NoError(t, os.WriteFile(skillDir, []byte(flatSkillBody("v1")), 0o644))
	db, err := a.idb.Load(a.cacheDir)
	require.NoError(t, err)
	pkg := db.FindPackage(marketName, "skills/flat.md")
	if pkg == nil {
		pkg = db.FindPackage(marketName, refProfile(flatSkillRef))
	}
	require.NotNil(t, pkg)
	for li := range pkg.Locations {
		for fi := range pkg.Locations[li].Files {
			pkg.Locations[li].Files[fi].Path = ".claude/skills/flat"
		}
	}
	require.NoError(t, a.idb.Lock(a.cacheDir))
	require.NoError(t, a.idb.Save(a.cacheDir, db))
	require.NoError(t, a.idb.Unlock(a.cacheDir))

	f.push(t, a, map[string]string{"skills/flat.md": flatSkillBody("v2")})
	_, err = a.Update(service.UpdateOpts{})
	require.NoError(t, err)

	require.Contains(t, readFile(t, filepath.Join(skillDir, "SKILL.md")), "v2")
}

func TestRestoreDeleted_FlatSkill(t *testing.T) {
	_, a, p := installFlatSkill(t)

	skillMd := filepath.Join(p, ".claude/skills/flat/SKILL.md")
	require.NoError(t, os.Remove(skillMd))
	deleted, err := a.DetectDeleted(service.DetectDeletedOpts{})
	require.NoError(t, err)
	require.Len(t, deleted, 1)

	results, err := a.RestoreDeleted(deleted, service.RestoreOpts{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "restored", results[0].Action, "%v", results[0].Err)
	require.Contains(t, readFile(t, skillMd), "v1")
}

func TestRemove_FlatSkillRemovesDirectory(t *testing.T) {
	_, a, p := installFlatSkill(t)

	_, err := a.Remove(flatSkillRef, service.RemoveOpts{})
	require.NoError(t, err)
	require.NoDirExists(t, filepath.Join(p, ".claude/skills/flat"))
	require.NoFileExists(t, filepath.Join(p, ".claude/skills/flat"))
}
