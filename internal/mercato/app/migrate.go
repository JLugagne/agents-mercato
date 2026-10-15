package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

type dryRunWriter struct{}

func (dryRunWriter) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (dryRunWriter) WriteFile(string, []byte) error       { return nil }
func (dryRunWriter) DeleteFile(string) error              { return nil }
func (dryRunWriter) DeleteAll(string) error               { return nil }

// Migrate converts files written by older mct versions to the current
// layout: extension-less single-file skills, flat legacy hook entries in
// .claude/settings.json, and tool outputs from previous transformer formats
// or removed tools. Locally modified files are never overwritten or deleted.
// It is idempotent; a second run returns no result.
func (a *App) Migrate(opts service.MigrateOpts) ([]service.MigrateResult, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}
	if err := a.idb.Lock(a.cacheDir); err != nil {
		return nil, err
	}
	defer func() { _ = a.idb.Unlock(a.cacheDir) }()
	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}

	current := projectPath(cfg.LocalPath)
	var results []service.MigrateResult
	for _, location := range migrationLocations(db, current, opts.AllLocations) {
		results = append(results, a.migrateLocation(&db, cfg, location, opts.DryRun)...)
	}
	return results, nil
}

func migrationLocations(db domain.InstallDatabase, current string, all bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, im := range db.Markets {
		for _, pkg := range im.Packages {
			for _, loc := range pkg.Locations {
				if seen[loc.Path] || (!all && loc.Path != current) || !isExistingDir(loc.Path) {
					continue
				}
				seen[loc.Path] = true
				out = append(out, loc.Path)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (a *App) migrateLocation(db *domain.InstallDatabase, cfg domain.Config, location string, dryRun bool) []service.MigrateResult {
	var w txWriter = dryRunWriter{}
	commit := func() error { return nil }
	rollback := func() error { return nil }
	if !dryRun {
		var err error
		w, commit, rollback, err = a.beginWriter("migrate:" + location)
		if err != nil {
			return []service.MigrateResult{{Location: location, Err: err}}
		}
	}
	committed := false
	defer func() {
		if !committed {
			_ = rollback()
		}
	}()

	claudeDir := filepath.Join(location, filepath.Base(cfg.LocalPath))
	var results []service.MigrateResult

	if r, err := migrateLegacyHooks(w, *db, location, claudeDir); err != nil {
		return []service.MigrateResult{{Location: location, Err: err}}
	} else if r != nil {
		results = append(results, *r)
	}

	for mi := range db.Markets {
		im := &db.Markets[mi]
		mc := findMarketConfig(cfg, im.Market)
		for pi := range im.Packages {
			if im.Packages[pi].FindLocation(location) == nil {
				continue
			}
			r, err := a.migratePackage(w, im, pi, mc, location, claudeDir)
			if err != nil {
				return []service.MigrateResult{{Market: im.Market, Profile: im.Packages[pi].Profile, Location: location, Err: err}}
			}
			if len(r.Actions) > 0 || len(r.Warnings) > 0 {
				results = append(results, r)
			}
		}
	}

	if dryRun || len(results) == 0 {
		return results
	}
	if err := a.stageDBSave(w, *db); err != nil {
		return []service.MigrateResult{{Location: location, Err: err}}
	}
	if err := commit(); err != nil {
		return []service.MigrateResult{{Location: location, Err: err}}
	}
	committed = true
	return results
}

func migrateLegacyHooks(w txWriter, db domain.InstallDatabase, location, claudeDir string) (*service.MigrateResult, error) {
	hasHooks := false
	for _, im := range db.Markets {
		for _, pkg := range im.Packages {
			if len(pkg.Files.Hooks) > 0 && pkg.FindLocation(location) != nil {
				hasHooks = true
			}
		}
	}
	if !hasHooks {
		return nil, nil
	}
	settings := filepath.Join(claudeDir, "settings.json")
	st, err := loadHookState(w.ReadFile, settings)
	if err != nil {
		return nil, err
	}
	if !st.settingsDirty {
		return nil, nil
	}
	if err := st.save(w, settings); err != nil {
		return nil, err
	}
	return &service.MigrateResult{
		Location: location,
		Actions:  []string{"converted legacy hook entries in " + projectRelPath(location, settings) + " to matcher groups"},
	}, nil
}

func (a *App) migratePackage(w txWriter, im *domain.InstalledMarket, pkgIdx int, mc *domain.MarketConfig, location, claudeDir string) (service.MigrateResult, error) {
	pkg := &im.Packages[pkgIdx]
	res := service.MigrateResult{Market: im.Market, Profile: pkg.Profile, Location: location}

	for _, skill := range pkg.Files.Skills {
		action, err := migrateLegacyFlatSkill(w, pkg, location, claudeDir, skill)
		if err != nil {
			return res, err
		}
		if action != "" {
			res.Actions = append(res.Actions, action)
		}
	}

	if mc == nil || !a.hasLegacyToolRecords(*pkg, location) {
		return res, nil
	}
	old := toolRecordsAt(*pkg, location)
	protected := map[string]bool{}
	for _, f := range old {
		if modifiedLocally(w, location, f) {
			protected[f.Path] = true
		}
	}
	res.Warnings = append(res.Warnings, a.reTransformToolTargets(w, *mc, im, pkgIdx, pkg.Files, location, pkg.VersionAt(location), func(p string) bool { return protected[p] })...)
	res.Actions = append(res.Actions, toolMigrationActions(old, toolRecordsAt(*pkg, location), protected)...)
	return res, nil
}

func migrateLegacyFlatSkill(w txWriter, pkg *domain.InstalledPackage, location, claudeDir, skill string) (string, error) {
	legacy := filepath.Join(claudeDir, "skills", skill)
	info, err := os.Lstat(legacy)
	if err != nil || info.IsDir() {
		return "", nil
	}
	content, err := w.ReadFile(legacy)
	if err != nil {
		return "", err
	}
	skillMd, err := prepareFlatSkillDir(w, legacy)
	if err != nil {
		return "", err
	}
	if err := w.WriteFile(skillMd, content); err != nil {
		return "", err
	}
	oldRel, newRel := projectRelPath(location, legacy), projectRelPath(location, skillMd)
	if loc := findLocationByPathAndType(pkg, location, domain.RuntimeTypeClaudeCode); loc != nil {
		for i := range loc.Files {
			if loc.Files[i].Path == oldRel {
				loc.Files[i].Path = newRel
			}
		}
	}
	if !pkg.Files.IsFlatSkill(skill) {
		pkg.Files.FlatSkills = append(pkg.Files.FlatSkills, skill)
	}
	return fmt.Sprintf("moved single-file skill %s to %s", oldRel, newRel), nil
}

func (a *App) hasLegacyToolRecords(pkg domain.InstalledPackage, location string) bool {
	for _, loc := range pkg.Locations {
		if !isToolLocation(loc, location) {
			continue
		}
		if _, known := a.transformers.Get(loc.Type); !known && loc.Type != domain.RuntimeTypeSharedAgents {
			return true
		}
		for _, f := range loc.Files {
			if f.Source == "" && !f.IsHookPatch() {
				return true
			}
		}
	}
	return false
}

func toolRecordsAt(pkg domain.InstalledPackage, location string) map[string]domain.InstalledFile {
	out := map[string]domain.InstalledFile{}
	for _, loc := range pkg.Locations {
		if !isToolLocation(loc, location) {
			continue
		}
		for _, f := range loc.Files {
			out[loc.Type+"\x00"+toolRecordKey(f)] = f
		}
	}
	return out
}

func toolMigrationActions(before, after map[string]domain.InstalledFile, protected map[string]bool) []string {
	var actions []string
	oldPaths := map[string]string{}
	for _, f := range before {
		oldPaths[f.Path] = f.XXH
	}
	for _, key := range sortedKeys(after) {
		f := after[key]
		if xxh, ok := oldPaths[f.Path]; !ok || xxh != f.XXH {
			actions = append(actions, "generated "+f.Path)
		}
	}
	newPaths := map[string]bool{}
	for _, f := range after {
		newPaths[f.Path] = true
	}
	for _, key := range sortedKeys(before) {
		f := before[key]
		if !newPaths[f.Path] && !protected[f.Path] {
			actions = append(actions, "removed "+f.Path)
		}
	}
	return actions
}

func projectRelPath(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func migrationUpdateResults(migrated []service.MigrateResult, dryRun bool) []service.UpdateResult {
	action := "migrated"
	if dryRun {
		action = "would_migrate"
	}
	var out []service.UpdateResult
	for _, m := range migrated {
		ref := domain.MctRef(m.Location)
		if m.Market != "" {
			ref = domain.MctRef(m.Market + "@" + m.Profile)
		}
		r := service.UpdateResult{Ref: ref, Location: m.Location, Action: action, Details: m.Actions, Warnings: m.Warnings, Err: m.Err}
		if m.Err != nil {
			r.Action = "error"
		}
		out = append(out, r)
	}
	return out
}
